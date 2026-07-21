package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const fixtureRoot = "../../test/fixtures/workspace"

func TestDiscoverFromRoot(t *testing.T) {
	ws, err := Discover(fixtureRoot)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if ws.ID != "fixture-workspace" {
		t.Fatalf("unexpected id: %q", ws.ID)
	}
	if len(ws.Repositories) != 2 {
		t.Fatalf("expected 2 repositories, got %d", len(ws.Repositories))
	}
}

func TestDiscoverFromNestedDir(t *testing.T) {
	ws, err := Discover(filepath.Join(fixtureRoot, "repo-a"))
	if err != nil {
		t.Fatalf("Discover from nested dir: %v", err)
	}
	if ws.ID != "fixture-workspace" {
		t.Fatalf("unexpected id: %q", ws.ID)
	}
}

func TestDiscoverNotFound(t *testing.T) {
	if _, err := Discover(t.TempDir()); err == nil {
		t.Fatal("expected error when neither workspace.yaml nor a git repository is present")
	}
}

// TestDiscoverFallsBackToImplicitWorkspaceForPlainGitRepo confirms punakawan
// can attach to any git-tracked project with zero .punakawan/ scaffolding.
func TestDiscoverFallsBackToImplicitWorkspaceForPlainGitRepo(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")

	ws, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if ws.ID != filepath.Base(dir) {
		t.Errorf("ID = %q, want %q", ws.ID, filepath.Base(dir))
	}
	if len(ws.Repositories) != 1 || ws.Repositories[0].Path != "." {
		t.Fatalf("Repositories = %+v, want one entry with path \".\"", ws.Repositories)
	}
	if ws.Root != dir {
		t.Errorf("Root = %q, want %q", ws.Root, dir)
	}
}

// TestDiscoverFallsBackFromNestedDirInsideGitRepo confirms the fallback
// walks up to the git root, not just the starting directory.
func TestDiscoverFallsBackFromNestedDirInsideGitRepo(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	nested := filepath.Join(dir, "sub", "dir")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	ws, err := Discover(nested)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if ws.Root != dir {
		t.Errorf("Root = %q, want git root %q", ws.Root, dir)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestRepositoryPath(t *testing.T) {
	ws, err := Discover(fixtureRoot)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	path, err := ws.RepositoryPath("repo-a")
	if err != nil {
		t.Fatalf("RepositoryPath: %v", err)
	}
	want := filepath.Join(ws.Root, "repo-a")
	if path != want {
		t.Fatalf("got %q, want %q", path, want)
	}

	if _, err := ws.RepositoryPath("does-not-exist"); err == nil {
		t.Fatal("expected error for unknown repository id")
	}
}

func TestLoadGlobalConfigFromMissingFileReturnsEmpty(t *testing.T) {
	cfg, err := LoadGlobalConfigFrom(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("LoadGlobalConfigFrom: %v", err)
	}
	if len(cfg.Adapters) != 0 {
		t.Fatalf("expected no adapters, got %+v", cfg.Adapters)
	}
}

func TestLoadGlobalConfigFromParsesAdapters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "adapters:\n  atlassian:\n    command: node\n    args: [\"/path/to/run.js\"]\n    env_passthrough: [ATLASSIAN_API_TOKEN]\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadGlobalConfigFrom(path)
	if err != nil {
		t.Fatalf("LoadGlobalConfigFrom: %v", err)
	}
	atlassian, ok := cfg.Adapters["atlassian"]
	if !ok {
		t.Fatalf("expected an atlassian adapter entry, got %+v", cfg.Adapters)
	}
	if atlassian.Command != "node" || len(atlassian.Args) != 1 {
		t.Fatalf("unexpected atlassian config: %+v", atlassian)
	}
}

func TestMergeAdaptersProjectOverridesGlobal(t *testing.T) {
	global := &GlobalConfig{Adapters: map[string]AdapterConfig{
		"atlassian": {Command: "node", Args: []string{"/global/run.js"}},
		"docling":   {Command: "node", Args: []string{"/global/docling.js"}},
	}}
	ws := &Workspace{Adapters: map[string]AdapterConfig{
		"atlassian": {Command: "node", Args: []string{"/project/run.js"}},
	}}

	merged := ws.MergeAdapters(global)
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged adapters, got %+v", merged)
	}
	if merged["atlassian"].Args[0] != "/project/run.js" {
		t.Errorf("expected project override to win, got %+v", merged["atlassian"])
	}
	if merged["docling"].Args[0] != "/global/docling.js" {
		t.Errorf("expected global-only entry to survive, got %+v", merged["docling"])
	}
}

func TestPolicyPathDefault(t *testing.T) {
	ws, err := Discover(fixtureRoot)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := filepath.Join(ws.Root, ".punakawan", "policy.yaml")
	if got := ws.PolicyPath(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
