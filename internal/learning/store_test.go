package learning

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
)

// newTestStore opens the shared SQLite storage kernel in a temp dir and scopes
// a Store to a fixed test project id, mirroring approvals/taskstore test setup.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, "test-project")
}

func TestNormalizeKey(t *testing.T) {
	if NormalizeKey("Payout.Retry.Max_Attempts") != NormalizeKey("payout retry max attempts") {
		t.Fatal("normalization should collapse separators + case")
	}
	if NormalizeKey("a-b_c.d") != "a b c d" {
		t.Fatalf("got %q", NormalizeKey("a-b_c.d"))
	}
}

func TestFingerprintsDeterministicAndDistinct(t *testing.T) {
	if MetadataFingerprint("p", "test.command") != MetadataFingerprint("p", "TEST COMMAND") {
		t.Fatal("metadata fingerprint should normalize key")
	}
	if MetadataFingerprint("p", "a") == MetadataFingerprint("p", "b") {
		t.Fatal("distinct keys must differ")
	}
	if WorkflowFingerprint("p", []string{"a:x", "b:y"}) == WorkflowFingerprint("p", []string{"b:y", "a:x"}) {
		t.Fatal("step order is significant in the workflow graph fingerprint")
	}
	if KnowledgeFingerprint("p", "decision", "s", "h1") == KnowledgeFingerprint("p", "decision", "s", "h2") {
		t.Fatal("content hash must affect the knowledge fingerprint")
	}
}

func TestStoreDedupAnchor(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	p := Proposal{Id: "learn-1", ArtifactType: TypeMetadata, TargetId: "k", Fingerprint: "fp-1", SupportCount: 1, Status: StatusPending, CreatedAt: now, UpdatedAt: now}
	if err := s.Append(p); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.FindPendingByFingerprint("fp-1")
	if err != nil || !ok || got.Id != "learn-1" {
		t.Fatalf("dedup anchor not found: %v %v %+v", err, ok, got)
	}
	// An accepted proposal is no longer a dedup anchor.
	p.Status = StatusAccepted
	p.UpdatedAt = now.Add(time.Second)
	if err := s.Append(p); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.FindPendingByFingerprint("fp-1"); ok {
		t.Fatal("accepted proposal should not anchor dedup")
	}

	// Get returns the folded-latest state; List folds history to one row per id.
	got, ok, err = s.Get("learn-1")
	if err != nil || !ok {
		t.Fatalf("Get: %v ok=%v", err, ok)
	}
	if got.Status != StatusAccepted {
		t.Fatalf("Get returned stale status %q, want accepted", got.Status)
	}
	all, err := s.List()
	if err != nil || len(all) != 1 {
		t.Fatalf("List folded = %d rows (err %v), want 1", len(all), err)
	}
}

func TestListEmpty(t *testing.T) {
	s := newTestStore(t)
	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if all != nil {
		t.Fatalf("List on empty store = %+v, want nil", all)
	}
	if _, ok, err := s.Get("nope"); err != nil || ok {
		t.Fatalf("Get on empty store = ok %v (err %v), want not found", ok, err)
	}
}

// TestProjectScopingPreventsLeakage confirms two Stores over one *storage.DB
// with distinct project ids never see each other's proposals on any read path.
func TestProjectScopingPreventsLeakage(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	a := New(db, "project-a")
	b := New(db, "project-b")

	now := time.Now()
	p := Proposal{Id: "learn-1", ArtifactType: TypeMetadata, TargetId: "k", Fingerprint: "fp-1", SupportCount: 1, Status: StatusPending, CreatedAt: now, UpdatedAt: now}
	if err := a.Append(p); err != nil {
		t.Fatalf("Append in A: %v", err)
	}

	// B sees nothing on any read path.
	if list, err := b.List(); err != nil || len(list) != 0 {
		t.Fatalf("project B List = %+v (err %v), want empty", list, err)
	}
	if _, ok, err := b.Get("learn-1"); err != nil || ok {
		t.Fatalf("project B Get = ok %v (err %v), want not found", ok, err)
	}
	if _, ok, err := b.FindPendingByFingerprint("fp-1"); err != nil || ok {
		t.Fatalf("project B FindPendingByFingerprint = ok %v (err %v), want not found", ok, err)
	}

	// A still sees its own proposal.
	if list, err := a.List(); err != nil || len(list) != 1 {
		t.Fatalf("project A List = %+v (err %v), want 1", list, err)
	}
	if _, ok, err := a.FindPendingByFingerprint("fp-1"); err != nil || !ok {
		t.Fatalf("project A FindPendingByFingerprint = ok %v (err %v), want found", ok, err)
	}
}
