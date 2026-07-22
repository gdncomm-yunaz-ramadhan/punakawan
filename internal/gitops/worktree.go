package gitops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/internal/policy"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

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
// per §11.1's example layout (.punakawan/worktrees/<repoID>/<taskID>).
// Exported so callers that need to address a running task's worktree (e.g.
// file-editing tools) can derive the same path without duplicating the
// formula.
func WorktreePath(workspaceRoot, repoID, taskID string) string {
	return filepath.Join(workspaceRoot, ".punakawan", "worktrees", repoID, taskID)
}

// WorktreeManager creates and removes isolated worktrees, gated by approval
// (§16) and preceded by a workspace lock and clean-repository check (§11.1
// steps 1-3, §3.1 "Workspace locking").
//
// The plan's approval categories (§16.1) do not enumerate a dedicated
// worktree-creation category, so requests use
// ApprovalRecordOperationDestructiveFilesystemAction as the closest existing
// fit — creating a worktree mutates the filesystem outside the main working
// tree, even though it is not destructive in the harmful sense.
type WorktreeManager struct {
	sup       *tools.Supervisor
	inspector *Inspector
	approvals *approvals.Store
	policy    *policy.Policy
}

// NewWorktreeManager constructs a WorktreeManager.
func NewWorktreeManager(sup *tools.Supervisor, store *approvals.Store, pol *policy.Policy) *WorktreeManager {
	return &WorktreeManager{
		sup:       sup,
		inspector: NewInspector(sup),
		approvals: store,
		policy:    pol,
	}
}

func approvalID(repoID, taskID string) string {
	return fmt.Sprintf("approval-worktree-%s-%s", repoID, taskID)
}

// RequestApproval creates a pending approval record for creating a worktree
// for taskID in repoID, or returns the existing record if one was already
// requested (idempotent).
func (m *WorktreeManager) RequestApproval(runID, repoID, taskID string, requestedBy protocol.ApprovalRecordRequestedBy) (protocol.ApprovalRecord, error) {
	id := approvalID(repoID, taskID)

	current, err := m.approvals.Current()
	if err != nil {
		return protocol.ApprovalRecord{}, err
	}
	if rec, ok := current[id]; ok {
		return rec, nil
	}

	target := fmt.Sprintf("%s:%s", repoID, taskID)
	reason := fmt.Sprintf("create isolated git worktree for task %s in repository %s", taskID, repoID)
	rec := protocol.ApprovalRecord{
		Id:          id,
		RunId:       runID,
		Operation:   protocol.ApprovalRecordOperationDestructiveFilesystemAction,
		Target:      &target,
		Reason:      &reason,
		RequestedBy: requestedBy,
		Status:      protocol.ApprovalRecordStatusPending,
		CreatedAt:   time.Now().UTC(),
	}
	if err := m.approvals.Append(rec); err != nil {
		return protocol.ApprovalRecord{}, err
	}
	return rec, nil
}

// Approve marks a pending worktree-creation request as approved.
func (m *WorktreeManager) Approve(repoID, taskID, approvedBy string) error {
	return m.resolve(repoID, taskID, protocol.ApprovalRecordStatusApproved, approvedBy)
}

// Deny marks a pending worktree-creation request as denied.
func (m *WorktreeManager) Deny(repoID, taskID, approvedBy string) error {
	return m.resolve(repoID, taskID, protocol.ApprovalRecordStatusDenied, approvedBy)
}

func (m *WorktreeManager) resolve(repoID, taskID string, status protocol.ApprovalRecordStatus, approvedBy string) error {
	id := approvalID(repoID, taskID)
	current, err := m.approvals.Current()
	if err != nil {
		return err
	}
	rec, ok := current[id]
	if !ok {
		return fmt.Errorf("gitops: no approval request %q; call RequestApproval first", id)
	}

	now := time.Now().UTC()
	rec.Status = status
	rec.ApprovedBy = &approvedBy
	rec.ResolvedAt = &now
	return m.approvals.Append(rec)
}

// Create creates an isolated worktree and task branch for repoID/taskID.
// It requires a prior approved request (see RequestApproval/Approve),
// acquires a per-repository lock, and refuses to proceed if the base
// repository has uncommitted changes.
func (m *WorktreeManager) Create(ctx context.Context, workspaceRoot, repoPath, repoID, taskID string) (*Worktree, error) {
	current, err := m.approvals.Current()
	if err != nil {
		return nil, err
	}
	rec, ok := current[approvalID(repoID, taskID)]
	if !ok || rec.Status != protocol.ApprovalRecordStatusApproved {
		return nil, fmt.Errorf("gitops: worktree creation for task %q in repository %q is not approved", taskID, repoID)
	}

	release, err := m.acquireLock(workspaceRoot, repoID)
	if err != nil {
		return nil, err
	}
	defer release()

	status, err := m.inspector.Status(ctx, repoPath)
	if err != nil {
		return nil, fmt.Errorf("gitops: check base repository is clean: %w", err)
	}
	if !status.Clean {
		return nil, fmt.Errorf("gitops: repository %s has uncommitted changes; refusing to create a worktree", repoPath)
	}

	baseSHA, err := m.inspector.HeadSHA(ctx, repoPath)
	if err != nil {
		return nil, fmt.Errorf("gitops: resolve base commit: %w", err)
	}

	branch := "punakawan/" + taskID
	worktreeDir := WorktreePath(workspaceRoot, repoID, taskID)
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
func (m *WorktreeManager) Remove(ctx context.Context, repoPath string, wt *Worktree) error {
	res, err := m.sup.Run(ctx, tools.Spec{
		Name: "git",
		Args: []string{"worktree", "remove", "--force", wt.Path},
		Dir:  repoPath,
	})
	if err != nil {
		return fmt.Errorf("gitops: git worktree remove: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("gitops: git worktree remove failed: %s", res.Stderr)
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
