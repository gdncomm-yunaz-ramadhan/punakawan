package gitops

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/internal/policy"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// newCleanRepo creates a real git repository with one commit and no
// uncommitted changes, suitable for the worktree-creation happy path.
func newCleanRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")

	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "first commit")

	return dir
}

func newWorktreeManager(t *testing.T, repoRoot, workspaceRoot string) *WorktreeManager {
	t.Helper()
	sup := tools.New(repoRoot, workspaceRoot)
	store, err := approvals.Open(workspaceRoot)
	if err != nil {
		t.Fatalf("approvals.Open: %v", err)
	}
	return NewWorktreeManager(sup, store, policy.Default())
}

func TestWorktreeCreateWithoutApprovalFails(t *testing.T) {
	repo := newCleanRepo(t)
	workspace := t.TempDir()
	mgr := newWorktreeManager(t, repo, workspace)

	if _, err := mgr.Create(context.Background(), workspace, repo, "repo-a", "task-1"); err == nil {
		t.Fatal("expected Create to fail without an approved request")
	}
}

func TestWorktreeRequestApproveCreateRemove(t *testing.T) {
	repo := newCleanRepo(t)
	workspace := t.TempDir()
	mgr := newWorktreeManager(t, repo, workspace)

	rec, err := mgr.RequestApproval("run-1", "repo-a", "task-1", protocol.ApprovalRecordRequestedByPetruk)
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if rec.Status != protocol.ApprovalRecordStatusPending {
		t.Fatalf("expected pending status, got %q", rec.Status)
	}

	if err := mgr.Approve("repo-a", "task-1", "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	wt, err := mgr.Create(context.Background(), workspace, repo, "repo-a", "task-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if wt.Branch != "punakawan/task-1" {
		t.Fatalf("branch: got %q, want %q", wt.Branch, "punakawan/task-1")
	}
	if info, err := os.Stat(wt.Path); err != nil || !info.IsDir() {
		t.Fatalf("expected worktree dir to exist at %s: %v", wt.Path, err)
	}

	if err := mgr.Remove(context.Background(), repo, wt); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("expected worktree dir to be removed, stat err = %v", err)
	}
}

func TestWorktreeCreateRefusesDirtyRepo(t *testing.T) {
	repo := newTestRepo(t) // intentionally dirty, from inspect_test.go
	workspace := t.TempDir()
	mgr := newWorktreeManager(t, repo, workspace)

	if _, err := mgr.RequestApproval("run-1", "repo-a", "task-1", protocol.ApprovalRecordRequestedByPetruk); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := mgr.Approve("repo-a", "task-1", "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	if _, err := mgr.Create(context.Background(), workspace, repo, "repo-a", "task-1"); err == nil {
		t.Fatal("expected Create to refuse a dirty base repository")
	}
}

func TestWorktreeDeniedRequestCannotCreate(t *testing.T) {
	repo := newCleanRepo(t)
	workspace := t.TempDir()
	mgr := newWorktreeManager(t, repo, workspace)

	if _, err := mgr.RequestApproval("run-1", "repo-a", "task-1", protocol.ApprovalRecordRequestedByPetruk); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := mgr.Deny("repo-a", "task-1", "ygrip"); err != nil {
		t.Fatalf("Deny: %v", err)
	}

	if _, err := mgr.Create(context.Background(), workspace, repo, "repo-a", "task-1"); err == nil {
		t.Fatal("expected Create to fail after denial")
	}
}

func TestWorktreeLockPreventsConcurrentUse(t *testing.T) {
	repo := newCleanRepo(t)
	workspace := t.TempDir()
	mgr := newWorktreeManager(t, repo, workspace)

	release, err := mgr.acquireLock(workspace, "repo-a")
	if err != nil {
		t.Fatalf("acquireLock: %v", err)
	}
	defer release()

	if _, err := mgr.acquireLock(workspace, "repo-a"); err == nil {
		t.Fatal("expected second lock acquisition on the same repo to fail")
	}
}
