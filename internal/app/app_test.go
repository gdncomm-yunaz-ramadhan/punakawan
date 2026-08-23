package app

import (
	"os"
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

func TestLoadFailsOutsideWorkspace(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("expected Load to fail when no workspace is discoverable")
	}
}

func TestLoadOptionalSucceedsOutsideAnyProject(t *testing.T) {
	a, err := LoadOptional(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOptional: %v", err)
	}

	if a.Workspace == nil || !a.Workspace.Ephemeral {
		t.Fatalf("expected an ephemeral workspace, got %+v", a.Workspace)
	}
	if a.Policy == nil || a.Supervisor == nil || a.AdapterRegistry == nil || a.Inspector == nil || a.Worktrees == nil {
		t.Fatalf("expected every service to still be wired against the ephemeral root, got %+v", a)
	}

	root := a.Workspace.Root
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(root); err == nil {
		t.Fatalf("expected Close to remove the ephemeral root %s", root)
	}
}

func TestLoadOptionalPrefersARealProject(t *testing.T) {
	a, err := LoadOptional(fixtureWorkspace)
	if err != nil {
		t.Fatalf("LoadOptional: %v", err)
	}
	defer a.Close()

	if a.Workspace == nil || a.Workspace.Ephemeral || a.Workspace.ID != "fixture-workspace" {
		t.Fatalf("expected a real project to win over the ephemeral fallback, got %+v", a.Workspace)
	}
}
