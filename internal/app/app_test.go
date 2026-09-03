package app

import (
	"os"
	"path/filepath"
	"testing"
)

const fixtureWorkspace = "../../test/fixtures/workspace"

func TestLoadWiresServices(t *testing.T) {
	a, err := Load(fixtureWorkspace)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if a.Workspace == nil || a.Workspace.ID != "fixture-workspace" {
		t.Fatalf("unexpected workspace: %+v", a.Workspace)
	}
	if a.Policy == nil {
		t.Fatal("expected a default policy to be loaded when no policy.yaml exists")
	}
	if a.Supervisor == nil || a.AdapterRegistry == nil || a.Inspector == nil || a.Worktrees == nil {
		t.Fatalf("expected all services to be wired, got %+v", a)
	}

	path, err := a.RepoPath("repo-a")
	if err != nil {
		t.Fatalf("RepoPath: %v", err)
	}
	if path == "" {
		t.Fatal("expected a non-empty repository path")
	}
}

// Loading outside any project used to fail, and the one entrypoint that
// could not afford to fail got a fabricated temp root instead. Both are
// gone: there is one Load, and with no project above startDir it wires
// every service against the machine's own data directory.
func TestLoadOutsideAnyProjectIsGlobal(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("PUNAKAWAN_DATA_DIR", dataDir)

	a, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	if a.Workspace == nil || !a.Workspace.Global {
		t.Fatalf("expected the global workspace, got %+v", a.Workspace)
	}
	if a.Workspace.Root != dataDir {
		t.Fatalf("Root = %q, want the machine data dir %q", a.Workspace.Root, dataDir)
	}
	if a.Policy == nil || a.Supervisor == nil || a.AdapterRegistry == nil || a.Inspector == nil || a.Worktrees == nil {
		t.Fatalf("expected every service to be wired with no project in scope, got %+v", a)
	}

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(dataDir); err != nil {
		t.Fatalf("Close removed the machine data dir: %v", err)
	}
}

func TestLoadPrefersARealProjectOverTheGlobalWorkspace(t *testing.T) {
	a, err := Load(fixtureWorkspace)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer a.Close()

	if a.Workspace == nil || a.Workspace.Global || a.Workspace.ID != "fixture-workspace" {
		t.Fatalf("expected a real project to win over the global workspace, got %+v", a.Workspace)
	}
}

// TestLoadLeavesAGitRepositoryUntouched: opening a project must not write
// into it. Three stores used to MkdirAll on open, so every command that
// only reads - workspace show, doctor, and the panel listing every
// registered project - left a .punakawan directory behind in whatever
// repository it had been pointed at, including repositories the user had
// never opened themselves.
func TestLoadLeavesAGitRepositoryUntouched(t *testing.T) {
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	a, err := Load(root)
	if err != nil {
		t.Fatalf("app.Load: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	if _, err := os.Stat(filepath.Join(root, ".punakawan")); !os.IsNotExist(err) {
		t.Fatalf("loading the project created .punakawan in it: %v", err)
	}
}
