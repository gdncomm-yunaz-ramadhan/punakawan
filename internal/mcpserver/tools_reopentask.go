package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/beads"
)

// ReopenTaskInput is reopen_task's input.
type ReopenTaskInput struct {
	TaskId string `json:"task_id" jsonschema:"the Beads issue id to reopen"`
	Reason string `json:"reason" jsonschema:"why this is being reopened - e.g. the blocking finding text"`
}

// ReopenTaskOutput is reopen_task's output.
type ReopenTaskOutput struct {
	TaskId   string `json:"task_id"`
	Reopened bool   `json:"reopened"`
}

// reopenTaskHandler implements the "reopen" half of M8's reopen-or-create
// acceptance criterion ("high-severity findings reopen or create tasks") -
// the "create" half is already served by report_discovered_task, so this is
// the one genuinely missing capability rather than a new abstraction over
// both.
func reopenTaskHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ReopenTaskInput) (*mcp.CallToolResult, ReopenTaskOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ReopenTaskInput) (*mcp.CallToolResult, ReopenTaskOutput, error) {
		if err := beads.ReopenIssue(ctx, a.Supervisor, a.Workspace.Root, in.TaskId, in.Reason); err != nil {
			return nil, ReopenTaskOutput{}, fmt.Errorf("mcpserver: reopen_task: %w", err)
		}
		return nil, ReopenTaskOutput{TaskId: in.TaskId, Reopened: true}, nil
	}
}
