package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/execution"
	"github.com/ygrip/punakawan/internal/gitops"
)

// StartTaskExecutionInput is start_task_execution's input.
type StartTaskExecutionInput struct {
	RunId       string `json:"run_id"`
	TaskId      string `json:"task_id"`
	RepoId      string `json:"repo_id" jsonschema:"repository id as declared in the workspace"`
	RequestedBy string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong; who is requesting the worktree"`
}

// StartTaskExecutionOutput is start_task_execution's output.
type StartTaskExecutionOutput struct {
	WorktreePath string `json:"worktree_path"`
	Branch       string `json:"branch"`
	BaseSha      string `json:"base_sha"`
}

func startTaskExecutionHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, StartTaskExecutionInput) (*mcp.CallToolResult, StartTaskExecutionOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in StartTaskExecutionInput) (*mcp.CallToolResult, StartTaskExecutionOutput, error) {
		requestedBy, err := validateRequestedBy(in.RequestedBy)
		if err != nil {
			return nil, StartTaskExecutionOutput{}, err
		}

		repoPath, err := a.RepoPath(in.RepoId)
		if err != nil {
			return nil, StartTaskExecutionOutput{}, fmt.Errorf("mcpserver: resolve repository %q: %w", in.RepoId, err)
		}

		// Idempotent: returns the existing request if start_task_execution
		// was already called for this task. The actual approve/deny
		// decision happens out-of-band (§16 is a human-in-the-loop gate, not
		// something the same calling role can grant itself) -- Create below
		// fails clearly if it has not been approved yet.
		if _, err := a.Worktrees.RequestApproval(in.RunId, in.RepoId, in.TaskId, requestedBy); err != nil {
			return nil, StartTaskExecutionOutput{}, fmt.Errorf("mcpserver: request worktree approval: %w", err)
		}

		sess, err := execution.StartTaskExecution(ctx, a.Worktrees, a.Workspace.Root, repoPath, in.RepoId, in.RunId, in.TaskId)
		if err != nil {
			return nil, StartTaskExecutionOutput{}, fmt.Errorf("mcpserver: start task execution: %w", err)
		}

		return nil, StartTaskExecutionOutput{
			WorktreePath: sess.Worktree.Path,
			Branch:       sess.Worktree.Branch,
			BaseSha:      sess.Worktree.BaseSHA,
		}, nil
	}
}

// FinishTaskExecutionInput is finish_task_execution's input.
type FinishTaskExecutionInput struct {
	RunId  string `json:"run_id"`
	TaskId string `json:"task_id"`
	RepoId string `json:"repo_id"`
	Status string `json:"status" jsonschema:"one of committed|blocked, per §11.3's execution loop"`
	Reason string `json:"reason,omitempty" jsonschema:"why the task was blocked, if status is blocked"`
}

// finishTaskExecutionOutput is an empty result: finish_task_execution's
// only effect is removing the worktree and appending a journal event.
type finishTaskExecutionOutput struct{}

func finishTaskExecutionHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, FinishTaskExecutionInput) (*mcp.CallToolResult, finishTaskExecutionOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in FinishTaskExecutionInput) (*mcp.CallToolResult, finishTaskExecutionOutput, error) {
		// Validate the status enum up front (punokawan-4ae): downstream
		// FinishTaskExecution treats any non-"committed" value as failure, so
		// a typo like "commited" would silently be recorded as a failure
		// rather than rejected. Only committed|blocked are meaningful here.
		if in.Status != "committed" && in.Status != "blocked" {
			return nil, finishTaskExecutionOutput{}, fmt.Errorf("mcpserver: finish_task_execution: invalid status %q: must be one of committed, blocked", in.Status)
		}

		repoPath, err := a.RepoPath(in.RepoId)
		if err != nil {
			return nil, finishTaskExecutionOutput{}, fmt.Errorf("mcpserver: resolve repository %q: %w", in.RepoId, err)
		}

		// start_task_execution and finish_task_execution are separate,
		// stateless MCP calls, so the session is reconstructed here from
		// deterministic paths rather than held in memory between calls:
		// gitops.WorktreePath is the same formula WorktreeManager.Create
		// used, and evidence.OpenJournal reopens the same run's journal
		// file (both idempotent to open repeatedly).
		journal, err := evidence.OpenJournal(a.Workspace.Root, in.RunId)
		if err != nil {
			return nil, finishTaskExecutionOutput{}, fmt.Errorf("mcpserver: open journal: %w", err)
		}
		sess := &execution.Session{
			RunID:    in.RunId,
			TaskID:   in.TaskId,
			RepoID:   in.RepoId,
			Worktree: &gitops.Worktree{Path: gitops.WorktreePath(a.Workspace.Root, in.RepoId, in.TaskId)},
			Journal:  journal,
		}

		var payload map[string]any
		if in.Reason != "" {
			payload = map[string]any{"reason": in.Reason}
		}
		if err := execution.FinishTaskExecution(ctx, a.Worktrees, repoPath, sess, in.Status, payload); err != nil {
			return nil, finishTaskExecutionOutput{}, fmt.Errorf("mcpserver: finish task execution: %w", err)
		}

		return nil, finishTaskExecutionOutput{}, nil
	}
}
