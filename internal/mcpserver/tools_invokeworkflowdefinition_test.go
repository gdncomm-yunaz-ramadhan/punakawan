package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/workflowdef"
)

// saveTestDefinition persists def directly through workflowdef.Store,
// the same store invoke_workflow and start_delivery's
// resolver both read from, without going through the
// save_workflow MCP tool (whose own validation
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
// invoke_workflow calls StartDelivery under the hood and
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
	callTool(t, cs, "invoke_workflow", map[string]any{
		"definition_id": "hotfix-delivery",
		"inputs": map[string]any{
			"references": []string{"JIRA-9001"},
		},
	}, &invoked)
	if invoked.RunId == "" {
		t.Fatal("invoke_workflow returned an empty run id")
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

func TestInvokeWorkflowAlwaysProducesDelivery(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	saveTestDefinition(t, a, workflowdef.Definition{
		Version: workflowdef.SchemaVersion,
		ID:      "plain-automation",
		Name:    "Plain Automation",
		Enabled: true,
		Steps:   []workflowdef.Step{{ID: "orient", Capability: "get_delivery"}},
	})

	var invoked InvokeWorkflowDefinitionOutput
	callTool(t, cs, "invoke_workflow", map[string]any{
		"definition_id": "plain-automation",
		"inputs":        map[string]any{"references": []string{"PAY-2"}},
	}, &invoked)
	if invoked.RunId == "" {
		t.Fatal("invoke_workflow returned an empty run id")
	}

	var got DeliveryViewOutput
	callTool(t, cs, "get_delivery", map[string]any{"orchestration_id": invoked.RunId}, &got)
	if got.View.Orchestration.WorkflowDefinitionId == nil || *got.View.Orchestration.WorkflowDefinitionId != "plain-automation" {
		t.Fatalf("workflow definition = %v, want plain-automation", got.View.Orchestration.WorkflowDefinitionId)
	}
	if got.View.PlanID == "" || got.View.PlanRevision != 1 {
		t.Fatalf("delivery plan = %s@%d, want first revision", got.View.PlanID, got.View.PlanRevision)
	}
}

// TestInvokeWorkflowJiraSourceRoutesThroughStartOrResolve: a delivery-shaped
// definition invoked with inputs.source naming a Jira source calls the
// same deliveryservice.Service.StartOrResolve path start_delivery's own
// source field uses, so invoking it twice for the same tenant+key reuses
// the same active lifetime instead of minting a second orchestration.
func TestInvokeWorkflowJiraSourceRoutesThroughStartOrResolve(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	saveTestDefinition(t, a, workflowdef.Definition{
		Version: workflowdef.SchemaVersion,
		ID:      "jira-source-delivery",
		Name:    "Jira Source Delivery",
		Enabled: true,
		Roles: map[string]workflowdef.RoleRestriction{
			"gareng": {Required: false},
		},
	})

	var first InvokeWorkflowDefinitionOutput
	callTool(t, cs, "invoke_workflow", map[string]any{
		"definition_id": "jira-source-delivery",
		"inputs": map[string]any{
			"source": map[string]any{"kind": "jira", "tenant": "tenant-a", "key": "SRC-1"},
		},
	}, &first)
	if first.RunId == "" {
		t.Fatal("invoke_workflow returned an empty run id")
	}

	var second InvokeWorkflowDefinitionOutput
	callTool(t, cs, "invoke_workflow", map[string]any{
		"definition_id": "jira-source-delivery",
		"inputs": map[string]any{
			"source": map[string]any{"kind": "jira", "tenant": "tenant-a", "key": "SRC-1"},
		},
	}, &second)
	if second.RunId != first.RunId {
		t.Fatalf("second invocation run id = %s, want %s (same active Jira lifetime reused)", second.RunId, first.RunId)
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
		"source":                 map[string]any{"kind": "jira", "tenant": "test-tenant", "key": "JIRA-1"},
		"plan":                   testPlan(),
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
		"source":                 map[string]any{"kind": "jira", "tenant": "test-tenant", "key": "JIRA-1"},
		"plan":                   testPlan(),
		"workflow_definition_id": "disabled-delivery",
	}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected start_delivery to fail for a disabled workflow_definition_id")
	}
}
