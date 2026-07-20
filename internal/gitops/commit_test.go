package gitops

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCommitTaskCommitsOnTaskBranch(t *testing.T) {
	repo := newCleanRepo(t)
	workspace := t.TempDir()
	mgr := newWorktreeManager(t, repo, workspace)

	if _, err := mgr.RequestApproval("run-1", "repo-a", "task-1", "petruk"); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := mgr.Approve("repo-a", "task-1", "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	wt, err := mgr.Create(context.Background(), workspace, repo, "repo-a", "task-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if wt.BaseSHA == "" {
		t.Fatal("expected Create to record a base SHA")
	}

	if err := os.WriteFile(filepath.Join(wt.Path, "change.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write change.txt: %v", err)
	}

	result, err := mgr.CommitTask(context.Background(), wt, "add change.txt", true, nil)
	if err != nil {
		t.Fatalf("CommitTask: %v", err)
	}
	if result.BaseSHA != wt.BaseSHA {
		t.Fatalf("BaseSHA: got %q, want %q", result.BaseSHA, wt.BaseSHA)
	}
	if result.CommitSHA == "" || result.CommitSHA == result.BaseSHA {
		t.Fatalf("expected a new resulting commit SHA, got %q", result.CommitSHA)
	}
}

func TestCommitTaskRefusesWithoutPassingDiffCheck(t *testing.T) {
	repo := newCleanRepo(t)
	workspace := t.TempDir()
	mgr := newWorktreeManager(t, repo, workspace)

	if _, err := mgr.RequestApproval("run-1", "repo-a", "task-1", "petruk"); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := mgr.Approve("repo-a", "task-1", "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	wt, err := mgr.Create(context.Background(), workspace, repo, "repo-a", "task-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := mgr.CommitTask(context.Background(), wt, "should not commit", false, []string{"secret detected"}); err == nil {
		t.Fatal("expected CommitTask to refuse when diffAllowed is false")
	}
}

func TestCommitTaskRefusesOnNonTaskBranch(t *testing.T) {
	repo := newCleanRepo(t)
	workspace := t.TempDir()
	mgr := newWorktreeManager(t, repo, workspace)

	baseSHA, err := mgr.inspector.HeadSHA(context.Background(), repo)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	// Point directly at the base repository (on its default branch) rather
	// than a real task worktree, to exercise the branch-prefix guard.
	wt := &Worktree{Path: repo, Branch: "main", BaseSHA: baseSHA}

	if _, err := mgr.CommitTask(context.Background(), wt, "should not commit", true, nil); err == nil {
		t.Fatal("expected CommitTask to refuse committing on a non-task branch")
	}
}
