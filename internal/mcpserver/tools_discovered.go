package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/tasks"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// ReportDiscoveredTaskInput is report_discovered_task's input, per §10.4's
// discovery rule.
type ReportDiscoveredTaskInput struct {
	RequirementId        string   `json:"requirement_id" jsonschema:"the parent requirement this discovered work still serves"`
	DiscoveredFromTaskId string   `json:"discovered_from_task_id" jsonschema:"the task being executed when this work was discovered"`
	TaskId               string   `json:"task_id" jsonschema:"stable id for the new discovered task"`
	Repository           string   `json:"repository"`
	Scope                string   `json:"scope"`
	AcceptanceCriteria   []string `json:"acceptance_criteria" jsonschema:"at least one entry is required"`
	DefinitionOfDone     string   `json:"definition_of_done"`
	BeadsParent          string   `json:"beads_parent,omitempty"`
	BeadsLabels          []string `json:"beads_labels,omitempty" jsonschema:"discovered and needs-semar-review are always added automatically"`
}

func reportDiscoveredTaskHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ReportDiscoveredTaskInput) (*mcp.CallToolResult, protocol.TaskContract, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ReportDiscoveredTaskInput) (*mcp.CallToolResult, protocol.TaskContract, error) {
		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, protocol.TaskContract{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}

		parentReq, err := store.Get(in.RequirementId)
		if err != nil {
			return nil, protocol.TaskContract{}, fmt.Errorf("mcpserver: load requirement %q: %w", in.RequirementId, err)
		}

		contract, err := tasks.ReportDiscoveredWork(ctx, a.Supervisor, a.Workspace.Root, store, parentReq, in.DiscoveredFromTaskId, tasks.NewTaskContractInput{
			TaskID:             in.TaskId,
			Repository:         in.Repository,
			Scope:              in.Scope,
			AcceptanceCriteria: in.AcceptanceCriteria,
			DefinitionOfDone:   in.DefinitionOfDone,
			BeadsParent:        in.BeadsParent,
			BeadsType:          "task",
			BeadsLabels:        in.BeadsLabels,
		})
		if err != nil {
			return nil, protocol.TaskContract{}, fmt.Errorf("mcpserver: report_discovered_task: %w", err)
		}

		return nil, contract, nil
	}
}
