package workspace

import (
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
		t.Fatal("expected error when no workspace.yaml is present")
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
