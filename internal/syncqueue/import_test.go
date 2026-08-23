package syncqueue

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
)

// writeLegacyQueue writes entries as append-only JSONL (one object per line,
// the exact shape the pre-kernel queue wrote) at the legacy path under root.
func writeLegacyQueue(t *testing.T, root string, entries []Entry) string {
	t.Helper()
	dir := filepath.Join(root, ".punakawan", "syncqueue")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	path := filepath.Join(dir, "queue.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create legacy file: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			t.Fatalf("encode legacy entry: %v", err)
		}
	}
	return path
}

func TestImportLegacyImportsAndRenames(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	legacyPath := writeLegacyQueue(t, root, []Entry{
		{Id: "s1", RunId: "r1", Adapter: "atlassian", Op: "atlassian.transition", IssueIdOrKey: "PAY-1", Error: "timeout", Attempts: 1, Status: StatusPending, CreatedAt: now},
		// A later line resolving s1 must win over the earlier pending line,
		// which only holds if import preserves original file order.
		{Id: "s1", RunId: "r1", Adapter: "atlassian", Op: "atlassian.transition", IssueIdOrKey: "PAY-1", Error: "timeout", Attempts: 1, Status: StatusResolved, CreatedAt: now, ResolvedAt: &now},
		{Id: "s2", RunId: "r2", Adapter: "atlassian", Op: "atlassian.addWorklog", IssueIdOrKey: "PAY-2", Error: "boom", Attempts: 2, Status: StatusPending, CreatedAt: now},
	})

	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	q := New(db, "test-project")
	if warn := q.ImportLegacy(root); warn != nil {
		t.Fatalf("ImportLegacy: %v", warn)
	}

	all, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("List = %d entries, want 3 (full history replayed)", len(all))
	}
	current, err := q.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current["s1"].Status != StatusResolved {
		t.Fatalf("s1 folds to %q, want resolved (later line must win)", current["s1"].Status)
	}
	// Import replays through Append, not Enqueue, so Attempts is preserved
	// verbatim rather than recomputed.
	if current["s2"].Attempts != 2 {
		t.Fatalf("s2 Attempts = %d, want 2 preserved (Append path, not Enqueue)", current["s2"].Attempts)
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
	writeLegacyQueue(t, root, []Entry{
		{Id: "s1", RunId: "r1", Adapter: "atlassian", Op: "atlassian.transition", IssueIdOrKey: "PAY-1", Error: "timeout", Attempts: 1, Status: StatusPending, CreatedAt: now},
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

	q := New(db, "test-project")
	if warn := q.ImportLegacy(root); warn != nil {
		t.Fatalf("ImportLegacy with no legacy file = %v, want nil", warn)
	}
	all, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("List = %d, want 0 (no phantom import)", len(all))
	}
}
