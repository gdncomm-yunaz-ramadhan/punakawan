// tools_invokeworkflowdefinition.go gives workflowdef.Invoker's
// RunCreator seam its first MCP-reachable binding: invoke_workflow_definition
// resolves a saved definition and hands it to workflowdef.NewInvoker with a
// RunCreator that dispatches on the definition's own shape. A definition with
// a non-empty Roles map is delivery-shaped - its Roles/AllowedCapabilities/
// ApprovalPolicy are a configuration overlay over internal/delivery's own
// fixed lane/lease/role-stage sequence, not a step graph to execute - so it
// is invoked by calling StartDeliveryWithDefinition and returning the
// resulting orchestration id. Every other definition keeps going through
// internal/workflow's existing run engine, the same way the panel's own
// invoke endpoint already creates a run today, unchanged.
package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/workcontext"
	"github.com/ygrip/punakawan/internal/workflow"
	"github.com/ygrip/punakawan/internal/workflowdef"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// InvokeWorkflowDefinitionInput is invoke_workflow_definition's input.
type InvokeWorkflowDefinitionInput struct {
	DefinitionId string         `json:"definition_id"`
	Inputs       map[string]any `json:"inputs,omitempty" jsonschema:"the definition's declared inputs; a delivery-shaped definition (non-empty roles) requires a references array, matching start_delivery's own input"`
}

// InvokeWorkflowDefinitionOutput is invoke_workflow_definition's output:
// one id whose shape depends on what was invoked - a delivery
// orchestration id (fetchable via get_delivery) for a delivery-shaped
// definition, or a legacy workflow run id (fetchable via
// get_next_workflow_step/get_workflow_state) otherwise.
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
			return nil, InvokeWorkflowDefinitionOutput{}, fmt.Errorf("mcpserver: invoke_workflow_definition: %w", err)
		}
		caps := workflowdef.NewCapabilitySet(CapabilityRegistry(a).Names(), nil)
		invoker := workflowdef.NewInvoker(caps, shapeAwareRunCreator(a))
		runID, err := invoker.Invoke(ctx, def, in.Inputs)
		if err != nil {
			return nil, InvokeWorkflowDefinitionOutput{}, fmt.Errorf("mcpserver: invoke_workflow_definition: %w", err)
		}
		return nil, InvokeWorkflowDefinitionOutput{RunId: runID}, nil
	}
}

// shapeAwareRunCreator is the second RunCreator workflowdef.Invoker
// gains alongside the panel's existing legacy one: a definition with a
// non-empty Roles map is delivery-shaped and routes to
// createDeliveryRun; every other definition keeps going through
// createLegacyWorkflowRun exactly as before. Roles is the one signal
// checked - a definition is either delivery-shaped by that one test, or
// it is not, with no further capability-taxonomy guessing in between: a
// definition that mixes delivery roles with steps meant for the general
// capability-DAG engine is not something this can safely infer its way
// out of, so it is simply treated as delivery-shaped rather than guessed
// at.
func shapeAwareRunCreator(a *app.App) workflowdef.RunCreator {
	return func(ctx context.Context, def workflowdef.Definition, inputs map[string]any) (string, error) {
		if len(def.Roles) > 0 {
			return createDeliveryRun(ctx, a, def, inputs)
		}
		return createLegacyWorkflowRun(ctx, a, def, inputs)
	}
}

// createDeliveryRun turns a delivery-shaped definition's inputs into a
// StartDeliveryWithDefinition call, carrying the definition's id onto
// the new orchestration so its role-stage gate can consult the
// definition's Roles map. It returns the orchestration id as this
// invocation's run id - a real, fetchable-via-get_delivery id, not a
// legacy workflow run id.
func createDeliveryRun(ctx context.Context, a *app.App, def workflowdef.Definition, inputs map[string]any) (string, error) {
	references, err := referencesFromInputs(inputs)
	if err != nil {
		return "", fmt.Errorf("delivery-shaped definition %q: %w", def.ID, err)
	}
	store, err := openDeliveryStore(ctx, a)
	if err != nil {
		return "", err
	}
	view, err := store.StartDeliveryWithDefinition(ctx, delivery.NewID(), references, def.ID)
	if err != nil {
		return "", fmt.Errorf("start delivery for definition %q: %w", def.ID, err)
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

// createLegacyWorkflowRun creates a WorkflowRun the same way the
// panel's own invoke endpoint does: compose the definition's bounded
// context, then stamp it onto a fresh run under the generic
// implementation-only carrier. internal/workflow's run engine itself is
// untouched by this - this is one more caller of its existing,
// already-used API, scoped to this server's own single workspace rather
// than the panel's cross-project runtime pool.
func createLegacyWorkflowRun(ctx context.Context, a *app.App, def workflowdef.Definition, inputs map[string]any) (string, error) {
	now := time.Now().UTC()
	prepared, err := workcontext.Prepare(workcontext.Request{
		WorkspaceRoot: a.Workspace.Root,
		Definitions:   []workflowdef.Definition{def},
		WorkflowID:    def.ID,
		Inputs:        inputs,
		Now:           now,
	}, nil, nil)
	if err != nil {
		return "", err
	}

	runID := fmt.Sprintf("pkw:run/%s/%s-%d", a.Workspace.ID, def.ID, now.UnixNano())
	run := workflow.New(runID, a.Workspace.ID, protocol.WorkflowRunWorkflowNameImplementationOnly, now)
	objective := def.Name
	run.Objective = &objective
	defRef := &protocol.WorkflowRunDefinitionRef{Id: def.ID, Revision: def.Revision, ContentHash: def.ContentHash()}
	run, err = workflow.StampContext(run, defRef, prepared.ResolvedInputs, prepared.StepProgress, &prepared.Snapshot, now)
	if err != nil {
		return "", fmt.Errorf("stamp context onto run for definition %q: %w", def.ID, err)
	}
	if err := a.Workflow.Append(run); err != nil {
		return "", fmt.Errorf("create workflow run for definition %q: %w", def.ID, err)
	}
	return runID, nil
}
