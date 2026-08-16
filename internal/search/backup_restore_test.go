package search

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/storage"
)

// canonicalHash hashes every record in store, sorted by id, so it stays
// comparable across a backup/restore round trip regardless of row order.
func canonicalHash(t *testing.T, store *knowledge.Store) string {
	t.Helper()
	records, _, err := store.ListRecords(context.Background(), knowledge.KnowledgeListQuery{})
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Id < records[j].Id })

	h := sha256.New()
	for _, r := range records {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal %s: %v", r.Id, err)
		}
		h.Write(b)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// TestBackupRestoreReproducesCanonicalRowsAndFTSResults covers the
// release gate's backup/restore requirement: write known records, back
// up the live database, lose the live database entirely, restore from
// the backup, and confirm both the canonical rows and FTS search results
// come back identical.
func TestBackupRestoreReproducesCanonicalRowsAndFTSResults(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "storage.db")

	db, err := storage.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	store := knowledge.New(db, "test-project")
	ix := newTestIndex(t)

	titles := []string{
		"Warehouse capacity threshold",
		"Ephemeral cache invalidation note",
		"Checkout latency regression",
	}
	for i, title := range titles {
		rec := newRecord(t, fmt.Sprintf("BR-%d", i))
		rec.Title = title
		putAndIndex(t, store, ix, rec)
	}

	preHash := canonicalHash(t, store)
	preResults, err := Search(store, ix, Request{Query: "checkout latency regression"})
	if err != nil {
		t.Fatalf("Search before backup: %v", err)
	}
	if len(preResults) == 0 || preResults[0].Id != "pkw:req/fixture/BR-2" {
		t.Fatalf("results before backup = %+v, want a match on BR-2", preResults)
	}

	backupPath := filepath.Join(dir, "backup.db")
	if err := db.Backup(ctx, backupPath); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// Lose the live database entirely - the scenario a restore exists for.
	if err := db.Close(); err != nil {
		t.Fatalf("close live db: %v", err)
	}
	if err := os.Remove(dbPath); err != nil {
		t.Fatalf("remove live db: %v", err)
	}
	if err := os.Rename(backupPath, dbPath); err != nil {
		t.Fatalf("restore backup over live path: %v", err)
	}

	restoredDB, err := storage.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("storage.Open restored db: %v", err)
	}
	defer restoredDB.Close()
	restoredStore := knowledge.New(restoredDB, "test-project")

	if postHash := canonicalHash(t, restoredStore); postHash != preHash {
		t.Fatalf("canonical row hash after restore = %s, want %s", postHash, preHash)
	}

	// The FTS index is a separate on-disk artifact from the canonical
	// database (internal/search, not something storage.Backup touches),
	// so recovering it means Rebuild-ing from the restored canonical
	// store - the same recovery path Rebuild already exists for.
	restoredIx := newTestIndex(t)
	if err := Rebuild(restoredStore, restoredIx); err != nil {
		t.Fatalf("Rebuild restored index: %v", err)
	}
	postResults, err := Search(restoredStore, restoredIx, Request{Query: "checkout latency regression"})
	if err != nil {
		t.Fatalf("Search after restore: %v", err)
	}
	if len(postResults) != len(preResults) || postResults[0].Id != preResults[0].Id {
		t.Fatalf("results after restore = %+v, want to match pre-backup results %+v", postResults, preResults)
	}
}
