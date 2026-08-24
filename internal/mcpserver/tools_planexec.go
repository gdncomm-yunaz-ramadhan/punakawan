// tools_planexec.go exposes internal/planexec: an alternative to the
// Beads-backed list_ready_tasks/claim_ready_task/reopen_task tools for a
// project that tracks execution against a Plan's own steps instead of (or
// alongside) Beads issues or the taskstore fallback. Nothing here reads
// or writes Beads/taskstore data, and none of the Beads-backed tools are
// changed or replaced by this - a caller picks whichever backend fits the
// project it is working in.
package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/planexec"
)

// PlanStepReadyInput is plan_step_ready's input.
type PlanStepReadyInput struct {
	PlanId string `json:"plan_id" jsonschema:"the plan id (plan_save/plan_get's plan.id) whose steps to check"`
}

// PlanStepReadyOutput is plan_step_ready's output.
type PlanStepReadyOutput struct {
	Executions []planexec.Execution `json:"executions"`
}

func planStepReadyHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, PlanStepReadyInput) (*mcp.CallToolResult, PlanStepReadyOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in PlanStepReadyInput) (*mcp.CallToolResult, PlanStepReadyOutput, error) {
		if in.PlanId == "" {
			return nil, PlanStepReadyOutput{}, fmt.Errorf("mcpserver: plan_step_ready: plan_id is required")
		}
		store, err := a.OpenPlanExec()
		if err != nil {
			return nil, PlanStepReadyOutput{}, fmt.Errorf("mcpserver: open plan step execution store: %w", err)
		}
		executions, err := store.ListReady(ctx, in.PlanId)
		if err != nil {
			return nil, PlanStepReadyOutput{}, err
		}
		return nil, PlanStepReadyOutput{Executions: executions}, nil
	}
}

// PlanStepClaimInput is plan_step_claim's input.
type PlanStepClaimInput struct {
	ExecutionId string `json:"execution_id" jsonschema:"the execution id to claim, as returned by plan_step_ready"`
	ClaimedBy   string `json:"claimed_by" jsonschema:"who is claiming this step, e.g. a session or agent identifier"`
}

// PlanStepClaimOutput is plan_step_claim's output.
type PlanStepClaimOutput struct {
	Execution planexec.Execution `json:"execution"`
}

func planStepClaimHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, PlanStepClaimInput) (*mcp.CallToolResult, PlanStepClaimOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in PlanStepClaimInput) (*mcp.CallToolResult, PlanStepClaimOutput, error) {
		store, err := a.OpenPlanExec()
		if err != nil {
			return nil, PlanStepClaimOutput{}, fmt.Errorf("mcpserver: open plan step execution store: %w", err)
		}
		exec, err := store.Claim(ctx, in.ExecutionId, in.ClaimedBy)
		if err != nil {
			return nil, PlanStepClaimOutput{}, err
		}
		return nil, PlanStepClaimOutput{Execution: exec}, nil
	}
}

// PlanStepCompleteInput is plan_step_complete's input.
type PlanStepCompleteInput struct {
	ExecutionId string `json:"execution_id" jsonschema:"the execution id to mark done"`
}

// PlanStepCompleteOutput is plan_step_complete's output.
type PlanStepCompleteOutput struct {
	Execution planexec.Execution `json:"execution"`
}

func planStepCompleteHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, PlanStepCompleteInput) (*mcp.CallToolResult, PlanStepCompleteOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in PlanStepCompleteInput) (*mcp.CallToolResult, PlanStepCompleteOutput, error) {
		store, err := a.OpenPlanExec()
		if err != nil {
			return nil, PlanStepCompleteOutput{}, fmt.Errorf("mcpserver: open plan step execution store: %w", err)
		}
		exec, err := store.Complete(ctx, in.ExecutionId)
		if err != nil {
			return nil, PlanStepCompleteOutput{}, err
		}
		return nil, PlanStepCompleteOutput{Execution: exec}, nil
	}
}

// PlanStepReopenInput is plan_step_reopen's input.
type PlanStepReopenInput struct {
	ExecutionId string `json:"execution_id" jsonschema:"the execution id to reopen"`
	Reason      string `json:"reason" jsonschema:"why this step is being reopened, e.g. a regression found in already-completed work"`
}

// PlanStepReopenOutput is plan_step_reopen's output.
type PlanStepReopenOutput struct {
	Execution planexec.Execution `json:"execution"`
}

func planStepReopenHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, PlanStepReopenInput) (*mcp.CallToolResult, PlanStepReopenOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in PlanStepReopenInput) (*mcp.CallToolResult, PlanStepReopenOutput, error) {
		store, err := a.OpenPlanExec()
		if err != nil {
			return nil, PlanStepReopenOutput{}, fmt.Errorf("mcpserver: open plan step execution store: %w", err)
		}
		exec, err := store.Reopen(ctx, in.ExecutionId, in.Reason)
		if err != nil {
			return nil, PlanStepReopenOutput{}, err
		}
		return nil, PlanStepReopenOutput{Execution: exec}, nil
	}
}
