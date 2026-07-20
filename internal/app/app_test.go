package app

import "testing"

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
	if a.Supervisor == nil || a.Approvals == nil || a.Inspector == nil || a.Worktrees == nil {
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
