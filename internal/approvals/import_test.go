package approvals

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// writeLegacyApprovals writes recs as append-only JSONL (one object per line,
// the exact shape the pre-kernel store wrote) at the legacy path under root.
func writeLegacyApprovals(t *testing.T, root string, recs []protocol.ApprovalRecord) string {
	t.Helper()
	dir := filepath.Join(root, ".punakawan", "approvals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	path := filepath.Join(dir, "approvals.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create legacy file: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range recs {
		if err := enc.Encode(r); err != nil {
			t.Fatalf("encode legacy record: %v", err)
		}
	}
	return path
}

func TestImportLegacyImportsAndRenames(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	legacyPath := writeLegacyApprovals(t, root, []protocol.ApprovalRecord{
		{Id: "a1", RunId: "r1", Operation: protocol.ApprovalRecordOperationGitPush, RequestedBy: protocol.ApprovalRecordRequestedByPetruk, Status: protocol.ApprovalRecordStatusPending, CreatedAt: now},
		// A later line resolving a1 must win over the earlier pending line,
		// which only holds if import preserves original file order.
		{Id: "a1", RunId: "r1", Operation: protocol.ApprovalRecordOperationGitPush, RequestedBy: protocol.ApprovalRecordRequestedByPetruk, Status: protocol.ApprovalRecordStatusApproved, CreatedAt: now},
		{Id: "a2", RunId: "r2", Operation: protocol.ApprovalRecordOperationGitPush, RequestedBy: protocol.ApprovalRecordRequestedByPetruk, Status: protocol.ApprovalRecordStatusPending, CreatedAt: now},
	})

	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	store := New(db, "test-project")
	if warn := store.ImportLegacy(root); warn != nil {
		t.Fatalf("ImportLegacy: %v", warn)
	}

	all, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("List = %d records, want 3 (full history replayed)", len(all))
	}
	current, err := store.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current["a1"].Status != protocol.ApprovalRecordStatusApproved {
		t.Fatalf("a1 folds to %q, want approved (later line must win)", current["a1"].Status)
	}

	// The legacy file is renamed to .imported and NOT deleted; the original
	// path no longer exists.
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy path still exists after import (err=%v), want gone", err)
	}
	if _, err := os.Stat(legacyPath + ".imported"); err != nil {
		t.Fatalf(".imported backup missing after import: %v", err)
	}
}

func TestImportLegacySecondOpenDoesNotDuplicate(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	writeLegacyApprovals(t, root, []protocol.ApprovalRecord{
		{Id: "a1", RunId: "r1", Operation: protocol.ApprovalRecordOperationGitPush, RequestedBy: protocol.ApprovalRecordRequestedByPetruk, Status: protocol.ApprovalRecordStatusPending, CreatedAt: now},
	})

	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if warn := New(db, "test-project").ImportLegacy(root); warn != nil {
		t.Fatalf("first ImportLegacy: %v", warn)
	}
	// Simulate a restart: a fresh store over the same db imports again. The
	// original legacy file was renamed away, so the rename now hits ENOENT and
	// the import is a correct no-op - no duplicate rows.
	if warn := New(db, "test-project").ImportLegacy(root); warn != nil {
		t.Fatalf("second ImportLegacy: %v", warn)
	}

	all, err := New(db, "test-project").List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("List = %d records after two opens, want 1 (no duplication)", len(all))
	}
}

func TestImportLegacyNoFileIsNoOp(t *testing.T) {
	root := t.TempDir() // no .punakawan/approvals/approvals.jsonl
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	store := New(db, "test-project")
	if warn := store.ImportLegacy(root); warn != nil {
		t.Fatalf("ImportLegacy with no legacy file = %v, want nil", warn)
	}
	all, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("List = %d, want 0 (no phantom import)", len(all))
	}
	// No stray .imported file was created out of nothing.
	if _, err := os.Stat(filepath.Join(root, ".punakawan", "approvals", "approvals.jsonl.imported")); !os.IsNotExist(err) {
		t.Fatalf("unexpected .imported file created (err=%v)", err)
	}
}
