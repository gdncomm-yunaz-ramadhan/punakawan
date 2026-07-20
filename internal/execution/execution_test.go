package execution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/policy"
	"github.com/ygrip/punakawan/internal/tools"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "first commit")
	return dir
}

func newManager(t *testing.T, repo, workspace string) *gitops.WorktreeManager {
	t.Helper()
	sup := tools.New(repo, workspace)
	store, err := approvals.Open(workspace)
	if err != nil {
		t.Fatalf("approvals.Open: %v", err)
	}
	return gitops.NewWorktreeManager(sup, store, policy.Default())
}

func TestStartAndFinishTaskExecution(t *testing.T) {
	repo := newRepo(t)
	workspace := t.TempDir()
	mgr := newManager(t, repo, workspace)

	if _, err := mgr.RequestApproval("run-1", "repo-a", "task-1", "petruk"); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := mgr.Approve("repo-a", "task-1", "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	sess, err := StartTaskExecution(context.Background(), mgr, workspace, repo, "repo-a", "run-1", "task-1")
	if err != nil {
		t.Fatalf("StartTaskExecution: %v", err)
	}
	if sess.Worktree == nil || sess.Bundle == nil || sess.Journal == nil {
		t.Fatal("expected a fully populated session")
	}
	if info, err := os.Stat(sess.Worktree.Path); err != nil || !info.IsDir() {
		t.Fatalf("expected worktree to exist at %s: %v", sess.Worktree.Path, err)
	}

	events, err := sess.Journal.List()
	if err != nil {
		t.Fatalf("Journal.List: %v", err)
	}
	if len(events) != 1 || events[0].Operation != "task-started" {
		t.Fatalf("expected one task-started event, got %+v", events)
	}

	if err := FinishTaskExecution(context.Background(), mgr, repo, sess, "committed", map[string]any{"commit_sha": "abc123"}); err != nil {
		t.Fatalf("FinishTaskExecution: %v", err)
	}

	if _, err := os.Stat(sess.Worktree.Path); !os.IsNotExist(err) {
		t.Fatalf("expected worktree to be removed, stat err = %v", err)
	}

	events, err = sess.Journal.List()
	if err != nil {
		t.Fatalf("Journal.List: %v", err)
	}
	if len(events) != 2 || events[1].Operation != "task-finished-committed" {
		t.Fatalf("expected a task-finished-committed event, got %+v", events)
	}
	if events[1].Result != "success" {
		t.Fatalf("expected a committed finish to record success, got %q", events[1].Result)
	}
}

func TestFinishTaskExecutionRecordsBlockedAsFailure(t *testing.T) {
	repo := newRepo(t)
	workspace := t.TempDir()
	mgr := newManager(t, repo, workspace)

	if _, err := mgr.RequestApproval("run-1", "repo-a", "task-2", "petruk"); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := mgr.Approve("repo-a", "task-2", "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	sess, err := StartTaskExecution(context.Background(), mgr, workspace, repo, "repo-a", "run-1", "task-2")
	if err != nil {
		t.Fatalf("StartTaskExecution: %v", err)
	}

	if err := FinishTaskExecution(context.Background(), mgr, repo, sess, "blocked", map[string]any{"reason": "tests failed"}); err != nil {
		t.Fatalf("FinishTaskExecution: %v", err)
	}

	events, err := sess.Journal.List()
	if err != nil {
		t.Fatalf("Journal.List: %v", err)
	}
	if events[len(events)-1].Result != "failure" {
		t.Fatalf("expected a blocked finish to record failure, got %q", events[len(events)-1].Result)
	}
}

func TestStartTaskExecutionRequiresApproval(t *testing.T) {
	repo := newRepo(t)
	workspace := t.TempDir()
	mgr := newManager(t, repo, workspace)

	if _, err := StartTaskExecution(context.Background(), mgr, workspace, repo, "repo-a", "run-1", "task-3"); err == nil {
		t.Fatal("expected StartTaskExecution to fail without an approved worktree request")
	}
}
