package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/beads"
)

// ReadyTasksInput is list_ready_tasks's and claim_ready_task's shared input.
type ReadyTasksInput struct {
	Assignee      string   `json:"assignee,omitempty"`
	ExcludeLabels []string `json:"exclude_labels,omitempty"`
	ExcludeTypes  []string `json:"exclude_types,omitempty"`
}

// ReadyTasksOutput is list_ready_tasks's and claim_ready_task's shared
// output.
type ReadyTasksOutput struct {
	Issues []beads.ReadyIssue `json:"issues"`
}

func listReadyTasksHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ReadyTasksInput) (*mcp.CallToolResult, ReadyTasksOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ReadyTasksInput) (*mcp.CallToolResult, ReadyTasksOutput, error) {
		issues, err := beads.Ready(ctx, a.Supervisor, a.Workspace.Root, beads.ReadyOptions{
			Assignee:      in.Assignee,
			ExcludeLabels: in.ExcludeLabels,
			ExcludeTypes:  in.ExcludeTypes,
		})
		if err != nil {
			return nil, ReadyTasksOutput{}, fmt.Errorf("mcpserver: list ready tasks: %w", err)
		}
		return nil, ReadyTasksOutput{Issues: issues}, nil
	}
}

func claimReadyTaskHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ReadyTasksInput) (*mcp.CallToolResult, ReadyTasksOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ReadyTasksInput) (*mcp.CallToolResult, ReadyTasksOutput, error) {
		issues, err := beads.ClaimReady(ctx, a.Supervisor, a.Workspace.Root, beads.ReadyOptions{
			Assignee:      in.Assignee,
			ExcludeLabels: in.ExcludeLabels,
			ExcludeTypes:  in.ExcludeTypes,
		})
		if err != nil {
			return nil, ReadyTasksOutput{}, fmt.Errorf("mcpserver: claim ready task: %w", err)
		}
		return nil, ReadyTasksOutput{Issues: issues}, nil
	}
}
