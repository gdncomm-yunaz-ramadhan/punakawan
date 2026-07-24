package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/beads"
)

// defaultReadyLimit bounds list_ready_tasks' response when the caller gives no
// limit, so a large ready backlog cannot return an unbounded list
// (punokawan-ssu).
const defaultReadyLimit = 50

// ReadyTasksInput is list_ready_tasks's and claim_ready_task's shared input.
//
// Assignee is a filter on the *candidate* set, not the identity of the
// claimer: it restricts which issues are considered (bd's --assignee), and for
// claim_ready_task bd sets the claimed issue's assignee to the invoking bd
// user itself, regardless of this value (punokawan-270). Leave it empty to
// consider every ready issue.
type ReadyTasksInput struct {
	Assignee      string   `json:"assignee,omitempty" jsonschema:"filter candidate issues by their current assignee; NOT the claimer - claim_ready_task assigns the claimed issue to the invoking bd user"`
	ExcludeLabels []string `json:"exclude_labels,omitempty"`
	ExcludeTypes  []string `json:"exclude_types,omitempty"`
	// Limit caps how many ready issues list_ready_tasks returns. Non-positive
	// or omitted defaults to defaultReadyLimit. Ignored by claim_ready_task,
	// which claims exactly one issue.
	Limit int `json:"limit,omitempty" jsonschema:"maximum number of ready issues to return (list_ready_tasks only); defaults to 50"`
}

// ReadyTasksOutput is list_ready_tasks's output: the ready issues matching the
// filters, capped at the requested limit.
type ReadyTasksOutput struct {
	Issues []beads.ReadyIssue `json:"issues"`
}

// ClaimReadyTaskOutput is claim_ready_task's output. It claims exactly one
// issue, so it exposes a single Issue rather than the plural Issues of
// list_ready_tasks (punokawan-270). Claimed is false with a nil Issue when no
// ready issue matched the filters (not an error - there was simply nothing to
// claim).
type ClaimReadyTaskOutput struct {
	Claimed bool              `json:"claimed"`
	Issue   *beads.ReadyIssue `json:"issue,omitempty"`
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

		limit := in.Limit
		if limit <= 0 {
			limit = defaultReadyLimit
		}
		if len(issues) > limit {
			issues = issues[:limit]
		}
		return nil, ReadyTasksOutput{Issues: issues}, nil
	}
}

func claimReadyTaskHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ReadyTasksInput) (*mcp.CallToolResult, ClaimReadyTaskOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ReadyTasksInput) (*mcp.CallToolResult, ClaimReadyTaskOutput, error) {
		issues, err := beads.ClaimReady(ctx, a.Supervisor, a.Workspace.Root, beads.ReadyOptions{
			Assignee:      in.Assignee,
			ExcludeLabels: in.ExcludeLabels,
			ExcludeTypes:  in.ExcludeTypes,
		})
		if err != nil {
			return nil, ClaimReadyTaskOutput{}, fmt.Errorf("mcpserver: claim ready task: %w", err)
		}
		// ClaimReady returns the single claimed issue as a one-element slice,
		// or an empty slice when nothing matched.
		if len(issues) == 0 {
			return nil, ClaimReadyTaskOutput{Claimed: false}, nil
		}
		return nil, ClaimReadyTaskOutput{Claimed: true, Issue: &issues[0]}, nil
	}
}
