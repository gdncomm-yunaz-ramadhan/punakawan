package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/diffcheck"
	"github.com/ygrip/punakawan/internal/gitops"
)

// CommitTaskInput is commit_task's input. DiffAllowed/Violations are retained
// for backward compatibility but are no longer trusted: a caller could
// previously set diff_allowed: true without ever having run check_diff, which
// bypassed §15.4's hard requirement (punokawan-97t). The handler now re-runs
// the diff/secret check server-side against the worktree and uses that
// verdict authoritatively; whatever the caller passes here is ignored.
type CommitTaskInput struct {
	RepoId  string `json:"repo_id"`
	TaskId  string `json:"task_id"`
	Message string `json:"message"`
	// Deprecated: ignored. The server re-derives the diff verdict itself.
	DiffAllowed bool `json:"diff_allowed,omitempty" jsonschema:"ignored: the server re-runs the diff check itself and does not trust this value"`
	// Deprecated: ignored. The server re-derives the violations itself.
	Violations []string `json:"violations,omitempty" jsonschema:"ignored: the server re-runs the diff check itself"`
}

// CommitTaskOutput is commit_task's output.
type CommitTaskOutput struct {
	BaseSha   string `json:"base_sha"`
	CommitSha string `json:"commit_sha"`
}

func commitTaskHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CommitTaskInput) (*mcp.CallToolResult, CommitTaskOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CommitTaskInput) (*mcp.CallToolResult, CommitTaskOutput, error) {
		worktreePath := gitops.WorktreePath(a.Workspace.Root, in.RepoId, in.TaskId)

		// Re-run the diff/secret check server-side rather than trusting the
		// caller-supplied diff_allowed boolean (punokawan-97t). A nil evidence
		// bundle skips re-writing diff.patch - check_diff already recorded that
		// evidence; here we only need the pass/fail verdict and its violations.
		report, err := diffcheck.Check(ctx, a.Supervisor, worktreePath, a.Policy, nil)
		if err != nil {
			return nil, CommitTaskOutput{}, fmt.Errorf("mcpserver: commit_task: re-check diff: %w", err)
		}

		baseSHA, err := a.Inspector.HeadSHA(ctx, worktreePath)
		if err != nil {
			return nil, CommitTaskOutput{}, fmt.Errorf("mcpserver: resolve worktree base commit: %w", err)
		}
		branch, err := a.Inspector.CurrentBranch(ctx, worktreePath)
		if err != nil {
			return nil, CommitTaskOutput{}, fmt.Errorf("mcpserver: resolve worktree branch: %w", err)
		}
		wt := &gitops.Worktree{Path: worktreePath, Branch: branch, BaseSHA: baseSHA}

		result, err := a.Worktrees.CommitTask(ctx, wt, in.Message, report.Allowed, report.Violations)
		if err != nil {
			return nil, CommitTaskOutput{}, fmt.Errorf("mcpserver: commit_task: %w", err)
		}

		return nil, CommitTaskOutput{BaseSha: result.BaseSHA, CommitSha: result.CommitSHA}, nil
	}
}
