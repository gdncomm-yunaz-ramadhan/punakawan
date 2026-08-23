// tools_plan.go implements plan_save and plan_get, the primary MCP
// surface over the first-class Plan aggregate (internal/plan,
// punakawan-efficiency-project-hygiene-refactor-plan.md §4). submit_final_plan
// (tools_semar.go) is kept as a thin compatibility wrapper over the same
// Store rather than duplicated here.
package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/plan"
)

// PlanSaveInput is plan_save's input: the plan's content, minus the
// server-assigned Revision/PreviousRevision/CreatedAt (Store.Save
// overwrites those regardless of what a caller supplies). Reuse an
// existing plan's Id to append a clarifying revision to it; a fresh id
// (e.g. from a run id) starts a new lineage.
type PlanSaveInput struct {
	Plan plan.Plan `json:"plan" jsonschema:"the plan revision to save; revision, previous_revision, and created_at are server-assigned and any values supplied here are overwritten"`
}

// PlanSaveOutput is plan_save's output: the plan as actually persisted,
// with its server-assigned fields filled in.
type PlanSaveOutput struct {
	Plan plan.Plan `json:"plan"`
}

func planSaveHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, PlanSaveInput) (*mcp.CallToolResult, PlanSaveOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in PlanSaveInput) (*mcp.CallToolResult, PlanSaveOutput, error) {
		store, err := a.OpenPlan()
		if err != nil {
			return nil, PlanSaveOutput{}, fmt.Errorf("mcpserver: open plan store: %w", err)
		}
		saved, err := store.Save(ctx, in.Plan)
		if err != nil {
			return nil, PlanSaveOutput{}, err
		}
		return nil, PlanSaveOutput{Plan: saved}, nil
	}
}

// PlanGetInput is plan_get's input. Omit Revision for the plan's current
// (highest) revision.
type PlanGetInput struct {
	Id       string `json:"id"`
	Revision *int   `json:"revision,omitempty" jsonschema:"exact revision to fetch; omitted means the current (highest) revision"`
}

// PlanGetOutput is plan_get's output.
type PlanGetOutput struct {
	Plan plan.Plan `json:"plan"`
}

func planGetHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, PlanGetInput) (*mcp.CallToolResult, PlanGetOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in PlanGetInput) (*mcp.CallToolResult, PlanGetOutput, error) {
		if in.Id == "" {
			return nil, PlanGetOutput{}, fmt.Errorf("mcpserver: plan_get: id is required")
		}
		store, err := a.OpenPlan()
		if err != nil {
			return nil, PlanGetOutput{}, fmt.Errorf("mcpserver: open plan store: %w", err)
		}
		var got plan.Plan
		if in.Revision != nil {
			got, err = store.GetRevision(ctx, in.Id, *in.Revision)
		} else {
			got, err = store.Get(ctx, in.Id)
		}
		if err != nil {
			return nil, PlanGetOutput{}, err
		}
		return nil, PlanGetOutput{Plan: got}, nil
	}
}
