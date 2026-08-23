package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ygrip/punakawan/internal/policy"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/tools"
)

// ErrWorktreeDirty is returned (wrapped in a *WorktreeDirtyError) by
// WorktreeManager.Remove when the worktree has uncommitted changes:
// removal is refused rather than forced, so the worktree stays on disk
// and inspectable.
var ErrWorktreeDirty = errors.New("gitops: worktree has uncommitted changes")

// WorktreeDirtyError reports that Remove refused to delete a dirty
// worktree, per §3.5: the worktree remains recoverable, and this error
// carries the path/branch/base-SHA/current-HEAD detail needed to inspect
// or manually clean it up.
type WorktreeDirtyError struct {
	Worktree    *Worktree
	CurrentHEAD string
}

func (e *WorktreeDirtyError) Error() string {
	return fmt.Sprintf("gitops: worktree %s (branch %s, base %s, head %s) has uncommitted changes; refusing to remove",
		e.Worktree.Path, e.Worktree.Branch, e.Worktree.BaseSHA, e.CurrentHEAD)
}

func (e *WorktreeDirtyError) Unwrap() error { return ErrWorktreeDirty }

// Worktree is an isolated git worktree created for a single task, per §11.1.
type Worktree struct {
	Path   string
	Branch string
	// BaseSHA is the base repository's HEAD commit at the moment this
	// worktree was created, i.e. the commit the task branch forked from.
	// Recorded per §15.4 ("Record base commit and resulting commit").
	BaseSHA string
}

// WorktreePath returns the canonical on-disk path for a task's worktree,
// under Punakawan's central per-user worktrees directory rather than
// inside the managed repository. Exported so callers that need to address
// a running task's worktree (e.g. file-editing tools) can derive the same
// path without duplicating the formula.
func WorktreePath(repoID, taskID string) (string, error) {
	dir, err := storage.WorktreesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, repoID, taskID), nil
}

// WorktreeManager creates and removes isolated worktrees, preceded by a
// workspace lock (§3.1 "Workspace locking"). Creation is internal
// execution infrastructure and requires no human approval: the resolved
// base commit, not the main checkout's cleanliness, is the isolation
// boundary.
type WorktreeManager struct {
	sup       *tools.Supervisor
	inspector *Inspector
	policy    *policy.Policy
}

// NewWorktreeManager constructs a WorktreeManager.
func NewWorktreeManager(sup *tools.Supervisor, pol *policy.Policy) *WorktreeManager {
	return &WorktreeManager{
		sup:       sup,
		inspector: NewInspector(sup),
		policy:    pol,
	}
}

// Create creates an isolated worktree and task branch for repoID/taskID,
// forked from repoPath's current HEAD. It acquires a per-repository lock
// but does not require repoPath itself to be clean: a dirty main checkout
// is unrelated to a linked worktree created from a resolved commit, since
// the base SHA is the isolation boundary.
func (m *WorktreeManager) Create(ctx context.Context, workspaceRoot, repoPath, repoID, taskID string) (*Worktree, error) {
	release, err := m.acquireLock(workspaceRoot, repoID)
	if err != nil {
		return nil, err
	}
	defer release()

	baseSHA, err := m.inspector.HeadSHA(ctx, repoPath)
	if err != nil {
		return nil, fmt.Errorf("gitops: resolve base commit: %w", err)
	}

	branch := "punakawan/" + taskID
	worktreeDir, err := WorktreePath(repoID, taskID)
	if err != nil {
		return nil, fmt.Errorf("gitops: resolve worktree path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(worktreeDir), 0o755); err != nil {
		return nil, fmt.Errorf("gitops: create worktree parent directory: %w", err)
	}

	// A task commonly resumes across more than one start_task_execution/
	// finish_task_execution round (e.g. an implementation pass, then a
	// later test/review pass on the same task_id) - finish_task_execution
	// removes the worktree directory but intentionally leaves the branch
	// and its commits in place. Re-running "git worktree add -b <branch>"
	// for an already-existing branch fails, so check first and check the
	// existing branch out instead of trying to recreate it.
	branchExists, err := m.branchExists(ctx, repoPath, branch)
	if err != nil {
		return nil, fmt.Errorf("gitops: check existing task branch: %w", err)
	}

	args := []string{"worktree", "add"}
	if branchExists {
		args = append(args, worktreeDir, branch)
	} else {
		args = append(args, "-b", branch, worktreeDir)
	}

	res, err := m.sup.Run(ctx, tools.Spec{
		Name: "git",
		Args: args,
		Dir:  repoPath,
	})
	if err != nil {
		return nil, fmt.Errorf("gitops: git worktree add: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("gitops: git worktree add failed: %s", res.Stderr)
	}

	return &Worktree{Path: worktreeDir, Branch: branch, BaseSHA: baseSHA}, nil
}

// branchExists reports whether branch already exists as a local branch in
// repoPath.
func (m *WorktreeManager) branchExists(ctx context.Context, repoPath, branch string) (bool, error) {
	res, err := m.sup.Run(ctx, tools.Spec{
		Name: "git",
		Args: []string{"show-ref", "--verify", "--quiet", "refs/heads/" + branch},
		Dir:  repoPath,
	})
	if err != nil {
		return false, fmt.Errorf("git show-ref: %w", err)
	}
	return res.ExitCode == 0, nil
}

// Remove removes a previously created worktree from its base repository.
// It refuses (returning ErrWorktreeDirty, wrapped with path/branch/base
// SHA/current-HEAD detail) rather than forcing removal of a worktree with
// uncommitted changes, leaving it on disk for manual recovery. A
// successful removal is followed by "git worktree prune" so the base
// repository's own worktree bookkeeping never accumulates stale entries.
func (m *WorktreeManager) Remove(ctx context.Context, repoPath string, wt *Worktree) error {
	status, err := m.inspector.Status(ctx, wt.Path)
	if err != nil {
		return fmt.Errorf("gitops: check worktree status: %w", err)
	}
	if !status.Clean {
		head, _ := m.inspector.HeadSHA(ctx, wt.Path)
		return &WorktreeDirtyError{Worktree: wt, CurrentHEAD: head}
	}

	res, err := m.sup.Run(ctx, tools.Spec{
		Name: "git",
		Args: []string{"worktree", "remove", wt.Path},
		Dir:  repoPath,
	})
	if err != nil {
		return fmt.Errorf("gitops: git worktree remove: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("gitops: git worktree remove failed: %s", res.Stderr)
	}

	pruneRes, err := m.sup.Run(ctx, tools.Spec{
		Name: "git",
		Args: []string{"worktree", "prune"},
		Dir:  repoPath,
	})
	if err != nil {
		return fmt.Errorf("gitops: git worktree prune: %w", err)
	}
	if pruneRes.ExitCode != 0 {
		return fmt.Errorf("gitops: git worktree prune failed: %s", pruneRes.Stderr)
	}
	return nil
}

// acquireLock takes an exclusive, non-blocking lock on repoID within the
// workspace. It fails immediately (rather than waiting) if the repository is
// already locked by another operation, per §3.1 "Workspace locking".
func (m *WorktreeManager) acquireLock(workspaceRoot, repoID string) (release func(), err error) {
	dir := filepath.Join(workspaceRoot, ".punakawan", "locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("gitops: create lock directory: %w", err)
	}

	path := filepath.Join(dir, repoID+".lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("gitops: repository %q is locked by another operation", repoID)
		}
		return nil, fmt.Errorf("gitops: acquire lock: %w", err)
	}
	f.Close()

	return func() { _ = os.Remove(path) }, nil
}
