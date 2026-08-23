package execution

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/policy"
	"github.com/ygrip/punakawan/internal/storage"
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
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())
	worktreesDir, err := storage.WorktreesDir()
	if err != nil {
		t.Fatalf("storage.WorktreesDir: %v", err)
	}
	sup := tools.New(repo, workspace, worktreesDir)
	return gitops.NewWorktreeManager(sup, policy.Default())
}

func TestStartAndFinishTaskExecution(t *testing.T) {
	repo := newRepo(t)
	workspace := t.TempDir()
	mgr := newManager(t, repo, workspace)

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

// TestStartTaskExecutionSucceedsWithDirtyMainCheckout confirms that a
// dirty main checkout does not block worktree creation (PR1 §3.2/§3.3):
// the resolved base SHA, not the checkout's cleanliness, is the isolation
// boundary, and creation requires no human approval record.
func TestStartTaskExecutionSucceedsWithDirtyMainCheckout(t *testing.T) {
	repo := newRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("dirty the main checkout: %v", err)
	}
	workspace := t.TempDir()
	mgr := newManager(t, repo, workspace)

	sess, err := StartTaskExecution(context.Background(), mgr, workspace, repo, "repo-a", "run-1", "task-3")
	if err != nil {
		t.Fatalf("expected StartTaskExecution to succeed against a dirty main checkout: %v", err)
	}
	if info, err := os.Stat(sess.Worktree.Path); err != nil || !info.IsDir() {
		t.Fatalf("expected worktree to exist at %s: %v", sess.Worktree.Path, err)
	}
}
