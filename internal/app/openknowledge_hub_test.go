package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/hub"
)

// newThrowawayWorkspaceDir builds a fresh git repo dir - unlike
// fixtureWorkspace (a shared, checked-in fixture), this one is safe to write
// .punakawan/hub-ref.yaml into before Load ever sees it.
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
// workspace with no hub-ref.
func newThrowawayApp(t *testing.T) *App {
	t.Helper()
	a, err := Load(newThrowawayWorkspaceDir(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func requireDoltForAppTest(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}
}

func TestOpenKnowledgeWithoutHubRefUsesLegacyPerProjectPath(t *testing.T) {
	requireDoltForAppTest(t)
	a := newThrowawayApp(t)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	// Legacy path's dbName is always "knowledge" (filepath.Base of
	// .punakawan/knowledge) - confirm the connection actually targets it, not
	// silently something else, by querying the active database name.
	var dbName string
	if err := store.DB().QueryRow("SELECT DATABASE()").Scan(&dbName); err != nil {
		t.Fatalf("query current database: %v", err)
	}
	if dbName != "knowledge" {
		t.Fatalf("expected the unchanged legacy dbName 'knowledge', got %q", dbName)
	}
}

func TestOpenKnowledgeWithHubRefUsesTheHub(t *testing.T) {
	requireDoltForAppTest(t)

	dir := newThrowawayWorkspaceDir(t)
	hubDir := filepath.Join(t.TempDir(), "hub")
	if err := hub.Write(dir, hub.Ref{HubDir: hubDir, ProjectID: "test-project"}); err != nil {
		t.Fatalf("hub.Write: %v", err)
	}

	// The hub-ref must exist before Load builds the Supervisor's allowed
	// roots (Load reads it once, up front, to sandbox-permit hubDir) -
	// OpenKnowledge itself only decides hub vs legacy lazily on first call.
	a, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	var dbName string
	if err := store.DB().QueryRow("SELECT DATABASE()").Scan(&dbName); err != nil {
		t.Fatalf("query current database: %v", err)
	}
	if dbName != "test-project" {
		t.Fatalf("expected the hub-ref's project_id as the active database, got %q", dbName)
	}
	if _, err := os.Stat(filepath.Join(hubDir, "test-project.events")); err != nil {
		t.Fatalf("expected hub events dir to be created under hubDir: %v", err)
	}
}
