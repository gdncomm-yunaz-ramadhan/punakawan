package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/gitops"
)

// CommitTaskInput is commit_task's input. DiffAllowed/Violations must come
// from a prior check_diff call's output: this tool does not run the diff
// check itself, so a caller cannot bypass it by simply passing
// diff_allowed: true without having actually checked.
//
// This trust boundary is a known limitation: the MCP surface cannot itself
// verify the caller actually ran check_diff first. §15.4's hard requirement
// is enforced properly in-process by internal/gitops.CommitTask's own
// re-staging; this input shape mirrors that function's signature rather
// than re-deriving the check here, to avoid duplicating diffcheck's secret
// scan and policy walk a second time per commit.
type CommitTaskInput struct {
	RepoId      string   `json:"repo_id"`
	TaskId      string   `json:"task_id"`
	Message     string   `json:"message"`
	DiffAllowed bool     `json:"diff_allowed" jsonschema:"must be the allowed value from a prior check_diff call"`
	Violations  []string `json:"violations,omitempty" jsonschema:"the violations from a prior check_diff call, if diff_allowed is false"`
}

// CommitTaskOutput is commit_task's output.
type CommitTaskOutput struct {
	BaseSha   string `json:"base_sha"`
	CommitSha string `json:"commit_sha"`
}

func commitTaskHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CommitTaskInput) (*mcp.CallToolResult, CommitTaskOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CommitTaskInput) (*mcp.CallToolResult, CommitTaskOutput, error) {
		worktreePath := gitops.WorktreePath(a.Workspace.Root, in.RepoId, in.TaskId)

		baseSHA, err := a.Inspector.HeadSHA(ctx, worktreePath)
		if err != nil {
			return nil, CommitTaskOutput{}, fmt.Errorf("mcpserver: resolve worktree base commit: %w", err)
		}
		branch, err := a.Inspector.CurrentBranch(ctx, worktreePath)
		if err != nil {
			return nil, CommitTaskOutput{}, fmt.Errorf("mcpserver: resolve worktree branch: %w", err)
		}
		wt := &gitops.Worktree{Path: worktreePath, Branch: branch, BaseSHA: baseSHA}

		result, err := a.Worktrees.CommitTask(ctx, wt, in.Message, in.DiffAllowed, in.Violations)
		if err != nil {
			return nil, CommitTaskOutput{}, fmt.Errorf("mcpserver: commit_task: %w", err)
		}

		return nil, CommitTaskOutput{BaseSha: result.BaseSHA, CommitSha: result.CommitSHA}, nil
	}
}
