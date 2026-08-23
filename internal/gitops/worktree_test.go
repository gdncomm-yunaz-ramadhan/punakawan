package gitops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/policy"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/tools"
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

// newWorktreeManager builds a WorktreeManager whose central worktrees
// directory (per PR1's project-hygiene refactor) is isolated to a fresh
// temp dir for the duration of the test, and whose Supervisor is allowed
// to operate against both the test repo/workspace and that central dir.
func newWorktreeManager(t *testing.T, repoRoot, workspaceRoot string) *WorktreeManager {
	t.Helper()
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())
	worktreesDir, err := storage.WorktreesDir()
	if err != nil {
		t.Fatalf("storage.WorktreesDir: %v", err)
	}
	sup := tools.New(repoRoot, workspaceRoot, worktreesDir)
	return NewWorktreeManager(sup, policy.Default())
}

func TestWorktreeCreateRequiresNoApproval(t *testing.T) {
	repo := newCleanRepo(t)
	workspace := t.TempDir()
	mgr := newWorktreeManager(t, repo, workspace)

	wt, err := mgr.Create(context.Background(), workspace, repo, "repo-a", "task-1")
	if err != nil {
		t.Fatalf("expected Create to succeed without any approval record: %v", err)
	}
	if wt.Branch != "punakawan/task-1" {
		t.Fatalf("branch: got %q, want %q", wt.Branch, "punakawan/task-1")
	}
	if info, err := os.Stat(wt.Path); err != nil || !info.IsDir() {
		t.Fatalf("expected worktree dir to exist at %s: %v", wt.Path, err)
	}

	// The worktree lives under Punakawan's central data dir, not inside
	// the managed repository or workspace.
	worktreesDir, err := storage.WorktreesDir()
	if err != nil {
		t.Fatalf("storage.WorktreesDir: %v", err)
	}
	if !strings.HasPrefix(wt.Path, worktreesDir+string(filepath.Separator)) {
		t.Fatalf("expected worktree path %s to live under central worktrees dir %s", wt.Path, worktreesDir)
	}
}

func TestWorktreeCreateSucceedsWithDirtyRepo(t *testing.T) {
	repo := newTestRepo(t) // intentionally dirty, from inspect_test.go
	workspace := t.TempDir()
	mgr := newWorktreeManager(t, repo, workspace)

	// A dirty main checkout is unrelated to a linked worktree created from
	// a resolved commit (PR1 §3.2): the base SHA is the isolation
	// boundary, so Create must succeed regardless.
	if _, err := mgr.Create(context.Background(), workspace, repo, "repo-a", "task-1"); err != nil {
		t.Fatalf("expected Create to succeed against a dirty base repository: %v", err)
	}
}

func TestWorktreeRemoveCleanSucceedsAndPrunes(t *testing.T) {
	repo := newCleanRepo(t)
	workspace := t.TempDir()
	mgr := newWorktreeManager(t, repo, workspace)

	wt, err := mgr.Create(context.Background(), workspace, repo, "repo-a", "task-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := mgr.Remove(context.Background(), repo, wt); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("expected worktree dir to be removed, stat err = %v", err)
	}

	out := runGit(t, repo, "worktree", "list", "--porcelain")
	if strings.Contains(out, wt.Path) {
		t.Fatalf("expected git worktree list to no longer reference removed/pruned worktree, got:\n%s", out)
	}
}

func TestWorktreeRemoveDirtyFailsAndPreserves(t *testing.T) {
	repo := newCleanRepo(t)
	workspace := t.TempDir()
	mgr := newWorktreeManager(t, repo, workspace)

	wt, err := mgr.Create(context.Background(), workspace, repo, "repo-a", "task-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := os.WriteFile(filepath.Join(wt.Path, "uncommitted.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("write uncommitted file in worktree: %v", err)
	}

	err = mgr.Remove(context.Background(), repo, wt)
	if err == nil {
		t.Fatal("expected Remove to refuse a dirty worktree")
	}
	if !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("expected error to wrap ErrWorktreeDirty, got %v", err)
	}
	var dirtyErr *WorktreeDirtyError
	if !errors.As(err, &dirtyErr) {
		t.Fatalf("expected error to be a *WorktreeDirtyError, got %T: %v", err, err)
	}
	if dirtyErr.Worktree.Path != wt.Path || dirtyErr.Worktree.Branch != wt.Branch || dirtyErr.Worktree.BaseSHA != wt.BaseSHA {
		t.Fatalf("expected dirty error to carry the worktree's path/branch/base SHA, got %+v", dirtyErr.Worktree)
	}
	if dirtyErr.CurrentHEAD == "" {
		t.Fatal("expected dirty error to carry the worktree's current HEAD")
	}

	if info, err := os.Stat(wt.Path); err != nil || !info.IsDir() {
		t.Fatalf("expected dirty worktree to remain on disk and inspectable at %s: %v", wt.Path, err)
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
