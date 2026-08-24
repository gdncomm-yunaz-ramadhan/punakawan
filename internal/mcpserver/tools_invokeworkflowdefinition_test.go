package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/workflowdef"
)

// saveTestDefinition persists def directly through workflowdef.Store,
// the same store invoke_workflow_definition and start_delivery's
// resolver both read from, without going through the
// save_workflow_definition MCP tool (whose own validation/canonicalization
// is not what these tests are about).
func saveTestDefinition(t *testing.T, a *app.App, def workflowdef.Definition) workflowdef.Definition {
	t.Helper()
	store, err := workflowdef.Open(a.Workspace.Root)
	if err != nil {
		t.Fatalf("workflowdef.Open: %v", err)
	}
	saved, err := store.Save(def)
	if err != nil {
		t.Fatalf("Save(%s): %v", def.ID, err)
	}
	return saved
}

// TestInvokeWorkflowDefinitionDeliveryShapedProducesRealOrchestration:
// invoking a delivery-shaped definition (non-empty roles) through
// invoke_workflow_definition calls StartDelivery under the hood and
// returns a real orchestration id - one get_delivery can read back -
// rather than a legacy workflow run id.
func TestInvokeWorkflowDefinitionDeliveryShapedProducesRealOrchestration(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	saveTestDefinition(t, a, workflowdef.Definition{
		Version: workflowdef.SchemaVersion,
		ID:      "hotfix-delivery",
		Name:    "Hotfix Delivery",
		Enabled: true,
		Roles: map[string]workflowdef.RoleRestriction{
			"gareng": {Required: false},
			"petruk": {Required: false},
		},
	})

	var invoked InvokeWorkflowDefinitionOutput
	callTool(t, cs, "invoke_workflow_definition", map[string]any{
		"definition_id": "hotfix-delivery",
		"inputs": map[string]any{
			"references": []string{"JIRA-9001"},
		},
	}, &invoked)
	if invoked.RunId == "" {
		t.Fatal("invoke_workflow_definition returned an empty run id")
	}

	var got DeliveryViewOutput
	callTool(t, cs, "get_delivery", map[string]any{"orchestration_id": invoked.RunId}, &got)
	if got.View.Orchestration.Id != invoked.RunId {
		t.Fatalf("get_delivery returned a different orchestration: %+v", got.View.Orchestration)
	}
	if got.View.Orchestration.WorkflowDefinitionId == nil || *got.View.Orchestration.WorkflowDefinitionId != "hotfix-delivery" {
		t.Fatalf("expected orchestration.workflow_definition_id = hotfix-delivery, got %v", got.View.Orchestration.WorkflowDefinitionId)
	}
	if got.View.PlanID == "" {
		t.Fatal("expected the delivery to reference a plan_id, got none")
	}
	if got.View.PlanRevision == 0 {
		t.Fatal("expected the delivery to reference a plan_revision, got 0")
	}
}

// TestInvokeWorkflowDefinitionNonDeliveryShapedStillProducesLegacyRun:
// a definition with no roles at all keeps going through the pre-existing
// legacy run engine, unaffected by this feature - it must NOT produce a
// delivery orchestration.
func TestInvokeWorkflowDefinitionNonDeliveryShapedStillProducesLegacyRun(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	saveTestDefinition(t, a, workflowdef.Definition{
		Version: workflowdef.SchemaVersion,
		ID:      "plain-automation",
		Name:    "Plain Automation",
		Enabled: true,
		Steps: []workflowdef.Step{
			{ID: "orient", Capability: "build_context_dossier"},
		},
	})

	var invoked InvokeWorkflowDefinitionOutput
	callTool(t, cs, "invoke_workflow_definition", map[string]any{
		"definition_id": "plain-automation",
	}, &invoked)
	if invoked.RunId == "" {
		t.Fatal("invoke_workflow_definition returned an empty run id")
	}

	run, err := a.Workflow.Get(invoked.RunId)
	if err != nil {
		t.Fatalf("legacy run %q was not created in the workflow run store: %v", invoked.RunId, err)
	}
	if run.DefinitionRef == nil || run.DefinitionRef.Id != "plain-automation" {
		t.Fatalf("expected run.definition_ref.id = plain-automation, got %v", run.DefinitionRef)
	}
	if run.PlanRef == nil || run.PlanRef.Id == "" {
		t.Fatalf("expected run.plan_ref to reference an instantiated plan, got %v", run.PlanRef)
	}

	// get_delivery must not treat this as a delivery orchestration.
	ctx := context.Background()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_delivery", Arguments: map[string]any{"orchestration_id": invoked.RunId}})
	if err != nil {
		t.Fatalf("CallTool(get_delivery): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected get_delivery to fail for a legacy workflow run id")
	}
}

// TestStartDeliveryRejectsUnknownWorkflowDefinitionId: start_delivery
// given a workflow_definition_id that does not exist must fail closed
// rather than silently proceeding without it.
func TestStartDeliveryRejectsUnknownWorkflowDefinitionId(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "start_delivery", Arguments: map[string]any{
		"references":            []string{"JIRA-1"},
		"workflow_definition_id": "does-not-exist",
	}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected start_delivery to fail for a nonexistent workflow_definition_id")
	}
}

// TestStartDeliveryRejectsDisabledWorkflowDefinitionId: a definition
// that exists but is disabled must also be rejected at attach time, not
// silently ignored.
func TestStartDeliveryRejectsDisabledWorkflowDefinitionId(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)
	ctx := context.Background()

	saveTestDefinition(t, a, workflowdef.Definition{
		Version: workflowdef.SchemaVersion,
		ID:      "disabled-delivery",
		Name:    "Disabled Delivery",
		Enabled: false,
		Roles: map[string]workflowdef.RoleRestriction{
			"gareng": {Required: false},
		},
	})

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "start_delivery", Arguments: map[string]any{
		"references":            []string{"JIRA-1"},
		"workflow_definition_id": "disabled-delivery",
	}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected start_delivery to fail for a disabled workflow_definition_id")
	}
}
