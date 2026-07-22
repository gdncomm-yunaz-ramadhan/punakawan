package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// newLocalRemoteForRepo creates a bare repo, pushes repoPath's current
// branch to it as "origin", and sets refs/remotes/origin/HEAD, mirroring
// internal/gitops/capabilities_test.go's newLocalRemote (mcpserver has no
// access to gitops' unexported test helper, so this is a small standalone
// copy scoped to this test file).
func newLocalRemoteForRepo(t *testing.T, repoPath string) {
	t.Helper()
	bareDir := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, t.TempDir(), "init", "--bare", "-b", "main", bareDir)
	runGit(t, repoPath, "remote", "add", "origin", bareDir)
	runGit(t, repoPath, "push", "-u", "origin", "main")
	runGit(t, repoPath, "remote", "set-head", "origin", "-a")
}

func TestPushTaskBranchHandlerPushesApprovedBranch(t *testing.T) {
	a := newTestApp(t)
	repoPath, err := a.RepoPath("repo-a")
	if err != nil {
		t.Fatalf("RepoPath: %v", err)
	}
	newLocalRemoteForRepo(t, repoPath)

	if _, err := a.Worktrees.RequestApproval("run-1", "repo-a", "task-1", "petruk"); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := a.Worktrees.Approve("repo-a", "task-1", "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	wt, err := a.Worktrees.Create(context.Background(), a.Workspace.Root, repoPath, "repo-a", "task-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write change.txt: %v", err)
	}
	if _, err := a.Worktrees.CommitTask(context.Background(), wt, "add change.txt", true, nil); err != nil {
		t.Fatalf("CommitTask: %v", err)
	}

	_, out, err := pushTaskBranchHandler(a)(context.Background(), nil, PushTaskBranchInput{RunId: "run-1", RepoId: "repo-a", TaskId: "task-1"})
	if err != nil {
		t.Fatalf("pushTaskBranchHandler: %v", err)
	}
	if !out.Pushed || out.Branch != wt.Branch {
		t.Fatalf("out = %+v, want Pushed=true Branch=%q", out, wt.Branch)
	}
}

func TestPushTaskBranchHandlerRejectsExplicitUserOverride(t *testing.T) {
	a := newTestApp(t)
	repoPath, err := a.RepoPath("repo-a")
	if err != nil {
		t.Fatalf("RepoPath: %v", err)
	}
	newLocalRemoteForRepo(t, repoPath)

	if _, err := a.Worktrees.RequestApproval("run-1", "repo-a", "task-1", "petruk"); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := a.Worktrees.Approve("repo-a", "task-1", "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	wt, err := a.Worktrees.Create(context.Background(), a.Workspace.Root, repoPath, "repo-a", "task-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	disallow := false
	_, out, err := pushTaskBranchHandler(a)(context.Background(), nil, PushTaskBranchInput{RunId: "run-1", RepoId: "repo-a", TaskId: "task-1", AllowPush: &disallow})
	if err != nil {
		t.Fatalf("pushTaskBranchHandler: %v", err)
	}
	if out.Pushed {
		t.Fatal("Pushed = true, want false when allow_push=false overrides detected/repo-policy permission")
	}
	if out.Reason == "" {
		t.Fatal("Reason is empty, want an explanation for the disallowed push")
	}
	_ = wt
}
