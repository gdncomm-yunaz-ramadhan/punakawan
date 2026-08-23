package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// newThrowawayWorkspaceDir builds a fresh git repo dir - unlike
// fixtureWorkspace (a shared, checked-in fixture), this one is safe to mutate
// per test.
func newThrowawayWorkspaceDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-q", "-m", "init")
	return dir
}

// newThrowawayApp builds a minimal, real *App over a fresh throwaway
// workspace, with the shared storage kernel pointed at an isolated temp dir so
// the test never touches this machine's real data directory.
func newThrowawayApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())
	a, err := Load(newThrowawayWorkspaceDir(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

// TestOpenKnowledgeRoundTripsThroughTheSharedKernel confirms OpenKnowledge now
// returns a Store backed by the shared SQLite kernel (no external process, no
// hub-ref decision), scoped to the workspace id, that a record round-trips
// through.
func TestOpenKnowledgeRoundTripsThroughTheSharedKernel(t *testing.T) {
	a := newThrowawayApp(t)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}

	// OpenKnowledge memoizes: a second call returns the same *Store.
	store2, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge (second): %v", err)
	}
	if store != store2 {
		t.Fatal("expected OpenKnowledge to memoize and return the same store")
	}

	rec := protocol.KnowledgeRecord{
		Id:         "pkw:req/fixture/APP-1",
		Type:       protocol.KnowledgeRecordTypeRequirement,
		Status:     "active",
		Title:      "app-wired record",
		Source:     protocol.KnowledgeRecordSource{Provider: "test", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodManual},
		Validity:   protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateObserved},
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != rec.Title {
		t.Fatalf("round-trip mismatch: got %q, want %q", got.Title, rec.Title)
	}
}
