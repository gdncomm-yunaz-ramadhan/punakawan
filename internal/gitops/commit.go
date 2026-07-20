package gitops

import (
	"context"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/tools"
)

// CommitResult records the base and resulting commit SHAs for one task
// commit, per §15.4 ("Record base commit and resulting commit").
type CommitResult struct {
	BaseSHA   string
	CommitSHA string
}

// taskBranchPrefix is the prefix WorktreeManager.Create always uses for a
// task branch (see Create's `branch := "punakawan/" + taskID`). CommitTask
// uses it as a defense-in-depth check that it is never committing on a
// repository's default branch, per §15.4 ("No direct edits on the default
// branch") -- Create already only ever branches into this prefix, so this
// check should never trip in practice, but it costs nothing to assert.
const taskBranchPrefix = "punakawan/"

// CommitTask stages and commits wt's pending changes, refusing to do so
// unless diffAllowed is true (the caller must have run a diff/secret check,
// e.g. internal/diffcheck.Check, and it must have passed) and the worktree
// is on a task branch rather than the repository's default branch.
//
// diffAllowed and violations are accepted as plain values rather than by
// importing internal/diffcheck's Report type directly, to avoid a Go import
// cycle (diffcheck already imports gitops for its Inspector).
func (m *WorktreeManager) CommitTask(ctx context.Context, wt *Worktree, message string, diffAllowed bool, violations []string) (CommitResult, error) {
	if !diffAllowed {
		return CommitResult{}, fmt.Errorf("gitops: refusing to commit: diff check did not pass: %s", strings.Join(violations, "; "))
	}

	branch, err := m.inspector.CurrentBranch(ctx, wt.Path)
	if err != nil {
		return CommitResult{}, fmt.Errorf("gitops: determine current branch: %w", err)
	}
	if !strings.HasPrefix(branch, taskBranchPrefix) {
		return CommitResult{}, fmt.Errorf("gitops: refusing to commit on branch %q; expected a %q-prefixed task branch", branch, taskBranchPrefix)
	}

	addRes, err := m.sup.Run(ctx, tools.Spec{Name: "git", Args: []string{"add", "-A"}, Dir: wt.Path})
	if err != nil {
		return CommitResult{}, fmt.Errorf("gitops: git add -A: %w", err)
	}
	if addRes.ExitCode != 0 {
		return CommitResult{}, fmt.Errorf("gitops: git add -A failed: %s", addRes.Stderr)
	}

	commitRes, err := m.sup.Run(ctx, tools.Spec{Name: "git", Args: []string{"commit", "-m", message}, Dir: wt.Path})
	if err != nil {
		return CommitResult{}, fmt.Errorf("gitops: git commit: %w", err)
	}
	if commitRes.ExitCode != 0 {
		return CommitResult{}, fmt.Errorf("gitops: git commit failed: %s", commitRes.Stderr)
	}

	commitSHA, err := m.inspector.HeadSHA(ctx, wt.Path)
	if err != nil {
		return CommitResult{}, fmt.Errorf("gitops: resolve resulting commit: %w", err)
	}

	return CommitResult{BaseSHA: wt.BaseSHA, CommitSHA: commitSHA}, nil
}
