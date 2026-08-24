// invoke_workflow resolves a saved definition, snapshots it into a Plan, and
// always starts a Delivery. Workflow definitions no longer own a second
// runtime state machine.
package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/workflowdef"
)

// InvokeWorkflowDefinitionInput is invoke_workflow's input.
type InvokeWorkflowDefinitionInput struct {
	DefinitionId string         `json:"definition_id"`
	Inputs       map[string]any `json:"inputs,omitempty" jsonschema:"the definition's declared inputs; a delivery-shaped definition (non-empty roles) requires a references array, matching start_delivery's own input"`
}

// InvokeWorkflowDefinitionOutput returns a delivery orchestration id.
type InvokeWorkflowDefinitionOutput struct {
	RunId string `json:"run_id"`
}

func invokeWorkflowDefinitionHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, InvokeWorkflowDefinitionInput) (*mcp.CallToolResult, InvokeWorkflowDefinitionOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in InvokeWorkflowDefinitionInput) (*mcp.CallToolResult, InvokeWorkflowDefinitionOutput, error) {
		defStore, err := workflowdef.Open(a.Workspace.Root)
		if err != nil {
			return nil, InvokeWorkflowDefinitionOutput{}, fmt.Errorf("mcpserver: open workflow definition store: %w", err)
		}
		def, err := defStore.Get(in.DefinitionId)
		if err != nil {
			return nil, InvokeWorkflowDefinitionOutput{}, fmt.Errorf("mcpserver: invoke_workflow: %w", err)
		}
		caps := workflowdef.NewCapabilitySet(CapabilityRegistry(a).Names(), nil)
		invoker := workflowdef.NewInvoker(caps, CreateWorkflowRun(a))
		runID, err := invoker.Invoke(ctx, def, in.Inputs)
		if err != nil {
			return nil, InvokeWorkflowDefinitionOutput{}, fmt.Errorf("mcpserver: invoke_workflow: %w", err)
		}
		return nil, InvokeWorkflowDefinitionOutput{RunId: runID}, nil
	}
}

// CreateWorkflowRun is shared by MCP and panel invocation. It implements the
// single Workflow -> Plan -> Delivery path.
func CreateWorkflowRun(a *app.App) workflowdef.RunCreator {
	return func(ctx context.Context, def workflowdef.Definition, inputs map[string]any) (string, error) {
		planID, planRevision, err := instantiatePlan(ctx, a, def)
		if err != nil {
			return "", err
		}
		return createDeliveryRun(ctx, a, def, inputs, planID, planRevision)
	}
}

// instantiatePlan saves a new plan.Plan lineage built from def, so every
// invocation - delivery-shaped or not - references an exact
// plan_id+plan_revision rather than only the workflow definition it came
// from.
func instantiatePlan(ctx context.Context, a *app.App, def workflowdef.Definition) (string, int, error) {
	planStore, err := a.OpenPlan()
	if err != nil {
		return "", 0, fmt.Errorf("open plan store for definition %q: %w", def.ID, err)
	}
	saved, err := planStore.Save(ctx, plan.FromWorkflowDefinition(def))
	if err != nil {
		return "", 0, fmt.Errorf("instantiate plan for definition %q: %w", def.ID, err)
	}
	return saved.ID, saved.Revision, nil
}

// createDeliveryRun turns a delivery-shaped definition's inputs into a
// StartDeliveryWithOptions call, carrying the definition's id onto
// the new orchestration so its role-stage gate can consult the
// definition's Roles map, then attaches planID/planRevision via
// UpdateOrchestrationDetails (OrchestrationOptions itself carries no
// plan fields - StartDeliveryWithOptions has already minted the
// orchestration's revision by the time the plan exists). It returns the
// orchestration id as this invocation's run id - a real,
// fetchable-via-get_delivery id, not a legacy workflow run id. No title
// is passed: a definition's inputs carry only references, so the run
// gets the same derived title get_delivery would show for it anyway.
func createDeliveryRun(ctx context.Context, a *app.App, def workflowdef.Definition, inputs map[string]any, planID string, planRevision int) (string, error) {
	references, err := referencesFromInputs(inputs)
	if err != nil {
		return "", fmt.Errorf("delivery-shaped definition %q: %w", def.ID, err)
	}
	store, err := openDeliveryStore(ctx, a)
	if err != nil {
		return "", err
	}
	view, err := store.StartDeliveryWithOptions(ctx, delivery.NewID(), references, delivery.OrchestrationOptions{WorkflowDefinitionID: def.ID})
	if err != nil {
		return "", fmt.Errorf("start delivery for definition %q: %w", def.ID, err)
	}
	if _, err := store.UpdateOrchestrationDetails(ctx, delivery.NewID(), view.Orchestration.Id, view.Orchestration.Revision, delivery.OrchestrationDetails{
		PlanID:       &planID,
		PlanRevision: &planRevision,
	}); err != nil {
		return "", fmt.Errorf("attach plan to delivery for definition %q: %w", def.ID, err)
	}
	return view.Orchestration.Id, nil
}

// referencesFromInputs extracts inputs["references"] as a []string,
// matching start_delivery's own input shape. It accepts both []string
// (a caller that built inputs in Go) and []any (the shape a JSON-decoded
// map produces), so a delivery-shaped definition invoked over either
// path sees identical behavior.
func referencesFromInputs(inputs map[string]any) ([]string, error) {
	raw, ok := inputs["references"]
	if !ok {
		return nil, fmt.Errorf(`missing required input "references"`)
	}
	switch v := raw.(type) {
	case []string:
		return v, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf(`input "references" must be an array of strings`)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf(`input "references" must be an array of strings`)
	}
}
