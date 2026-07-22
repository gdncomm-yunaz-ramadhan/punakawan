package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// PushTaskBranchInput is push_task_branch's input.
type PushTaskBranchInput struct {
	RunId  string `json:"run_id"`
	RepoId string `json:"repo_id"`
	TaskId string `json:"task_id"`
	// Remote defaults to "origin".
	Remote string `json:"remote,omitempty"`
	// AllowPush is this call's user-permission override (§7.4): unset
	// (nil, the default) defers to detected capability and repository
	// policy; explicit false always wins regardless of what either of
	// those say.
	AllowPush *bool `json:"allow_push,omitempty" jsonschema:"explicit per-call push permission; false always blocks the push regardless of detected capability or repository policy"`
}

// PushTaskBranchOutput is push_task_branch's output.
type PushTaskBranchOutput struct {
	Pushed bool   `json:"pushed"`
	Reason string `json:"reason,omitempty"`
	Branch string `json:"branch,omitempty"`
}

// pushTaskBranchHandler pushes a task's branch to its remote (§8's "push
// branch" step, ahead of create_pr), gated by §7.4's detected ∩ repository
// policy ∩ user permission merge - never a force-push (see
// WorktreeManager.PushBranch). Must run before finish_task_execution
// removes the task's worktree.
func pushTaskBranchHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, PushTaskBranchInput) (*mcp.CallToolResult, PushTaskBranchOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in PushTaskBranchInput) (*mcp.CallToolResult, PushTaskBranchOutput, error) {
		remote := in.Remote
		if remote == "" {
			remote = "origin"
		}

		worktreePath := gitops.WorktreePath(a.Workspace.Root, in.RepoId, in.TaskId)
		caps, err := a.Inspector.DetectCapabilities(ctx, worktreePath, remote)
		if err != nil {
			return nil, PushTaskBranchOutput{}, fmt.Errorf("mcpserver: detect git capabilities: %w", err)
		}

		userPermission := gitops.DefaultExecutionPolicy(protocol.GitExecutionPolicySourceUser)
		if in.AllowPush != nil {
			userPermission.AllowPush = *in.AllowPush
		}
		repoPolicy := gitops.DefaultExecutionPolicy(protocol.GitExecutionPolicySourceRepositoryPolicy)

		merged := gitops.MergeExecutionPolicy(caps, repoPolicy, userPermission)
		if !merged.AllowPush {
			reason := "push disallowed"
			if merged.Reason != nil {
				reason = *merged.Reason
			}
			return nil, PushTaskBranchOutput{Pushed: false, Reason: reason}, nil
		}

		wt := &gitops.Worktree{Path: worktreePath}
		branch, err := a.Worktrees.PushBranch(ctx, wt, remote)
		if err != nil {
			return nil, PushTaskBranchOutput{Pushed: false, Reason: err.Error()}, nil
		}
		return nil, PushTaskBranchOutput{Pushed: true, Branch: branch}, nil
	}
}
