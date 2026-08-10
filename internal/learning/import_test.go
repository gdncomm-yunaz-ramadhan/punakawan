package learning

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
)

// writeLegacyProposals writes props as append-only JSONL (one object per line,
// the exact shape the pre-kernel store wrote) at the legacy path under root.
func writeLegacyProposals(t *testing.T, root string, props []Proposal) string {
	t.Helper()
	dir := filepath.Join(root, ".punakawan", "learning")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	path := filepath.Join(dir, "proposals.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create legacy file: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, p := range props {
		if err := enc.Encode(p); err != nil {
			t.Fatalf("encode legacy proposal: %v", err)
		}
	}
	return path
}

func TestImportLegacyImportsAndRenames(t *testing.T) {
	root := t.TempDir()
	base := time.Now().UTC().Truncate(time.Second)
	legacyPath := writeLegacyProposals(t, root, []Proposal{
		{Id: "p1", ArtifactType: TypeMetadata, TargetId: "k", Fingerprint: "fp1", Status: StatusPending, CreatedAt: base, UpdatedAt: base},
		// A later line accepting p1 must win over the earlier pending line,
		// which only holds if import preserves original file order.
		{Id: "p1", ArtifactType: TypeMetadata, TargetId: "k", Fingerprint: "fp1", Status: StatusAccepted, CreatedAt: base, UpdatedAt: base.Add(time.Minute)},
		{Id: "p2", ArtifactType: TypeKnowledge, TargetId: "k2", Fingerprint: "fp2", Status: StatusPending, CreatedAt: base, UpdatedAt: base},
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
	if len(all) != 2 {
		t.Fatalf("List = %d proposals (folded), want 2", len(all))
	}
	p1, ok, err := store.Get("p1")
	if err != nil || !ok {
		t.Fatalf("Get(p1): ok=%v err=%v", ok, err)
	}
	if p1.Status != StatusAccepted {
		t.Fatalf("p1 folds to %q, want accepted (later line must win)", p1.Status)
	}

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
	writeLegacyProposals(t, root, []Proposal{
		{Id: "p1", ArtifactType: TypeMetadata, TargetId: "k", Fingerprint: "fp1", Status: StatusPending, CreatedAt: now, UpdatedAt: now},
	})

	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if warn := New(db, "test-project").ImportLegacy(root); warn != nil {
		t.Fatalf("first ImportLegacy: %v", warn)
	}
	// Simulate a restart: the original legacy file was renamed away, so the
	// second import hits ENOENT and is a correct no-op.
	if warn := New(db, "test-project").ImportLegacy(root); warn != nil {
		t.Fatalf("second ImportLegacy: %v", warn)
	}

	all, err := New(db, "test-project").List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("List = %d after two opens, want 1 (no duplication)", len(all))
	}
}

func TestImportLegacyNoFileIsNoOp(t *testing.T) {
	root := t.TempDir()
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
}
