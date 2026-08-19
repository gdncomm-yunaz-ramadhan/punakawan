package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// callToolExpectingError calls a tool that is supposed to be refused and
// returns the refusal text, so a test can assert on why it was refused
// rather than only that it was.
func callToolExpectingError(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("CallTool(%s) succeeded; want a refusal", name)
	}
	return errorText(res)
}

// TestUpdateDeliveryEditsEveryDescriptiveField drives update_delivery
// over the wire: a delivery started with a title and description has
// each of its editable fields changed afterwards, and every one of them
// comes back on the returned view.
func TestUpdateDeliveryEditsEveryDescriptiveField(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	var started StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"references":  []string{"PAY-1842"},
		"title":       "migrate checkout",
		"description": "Checkout still calls the retired v1 capture endpoint.",
	}, &started)
	if started.View.Description != "Checkout still calls the retired v1 capture endpoint." {
		t.Fatalf("view.description = %q, want the description supplied at creation", started.View.Description)
	}

	// A real plan record to point at, produced the way a caller would
	// actually produce one.
	var plan SubmitOutput
	callTool(t, cs, "submit_final_plan", map[string]any{
		"id":         "run-1",
		"title":      "final plan",
		"final_plan": map[string]any{"requirements": []string{"r1"}, "acceptance_criteria": []string{"a1"}},
	}, &plan)
	if plan.Id == "" {
		t.Fatal("submit_final_plan returned no record id")
	}

	var updated DeliveryViewOutput
	callTool(t, cs, "update_delivery", map[string]any{
		"orchestration_id":  started.OrchestrationId,
		"expected_revision": started.View.Orchestration.Revision,
		"title":             "migrate checkout to the payments v2 API",
		"description":       "v1 capture is retired in Q3; checkout is the last caller.",
		"plan_record_id":    plan.Id,
		"session_id":        "pkw:run/smoke/adhoc-17",
	}, &updated)

	if updated.View.Title != "migrate checkout to the payments v2 API" {
		t.Fatalf("view.title = %q, want the edited title", updated.View.Title)
	}
	if updated.View.Description != "v1 capture is retired in Q3; checkout is the last caller." {
		t.Fatalf("view.description = %q, want the edited description", updated.View.Description)
	}
	if updated.View.PlanRecordID != plan.Id {
		t.Fatalf("view.plan_record_id = %q, want %q", updated.View.PlanRecordID, plan.Id)
	}
	if updated.View.SessionID != "pkw:run/smoke/adhoc-17" {
		t.Fatalf("view.session_id = %q, want the recorded session", updated.View.SessionID)
	}

	// The same values come back from a fresh read, so they were persisted
	// rather than only reflected in the mutating call's own response.
	var reread DeliveryViewOutput
	callTool(t, cs, "get_delivery", map[string]any{"orchestration_id": started.OrchestrationId}, &reread)
	if reread.View.Description != updated.View.Description || reread.View.PlanRecordID != plan.Id || reread.View.SessionID != updated.View.SessionID {
		t.Fatalf("get_delivery view = %+v, want the edits persisted", reread.View)
	}

	// A stale expected_revision conflicts instead of overwriting.
	msg := callToolExpectingError(t, cs, "update_delivery", map[string]any{
		"orchestration_id":  started.OrchestrationId,
		"expected_revision": started.View.Orchestration.Revision,
		"title":             "written against a stale view",
	})
	if !strings.Contains(msg, "revision conflict") {
		t.Fatalf("refusal = %q, want it to name the revision conflict", msg)
	}

	// A plan reference nothing can resolve is refused rather than stored
	// as a dangling id.
	msg = callToolExpectingError(t, cs, "update_delivery", map[string]any{
		"orchestration_id":  started.OrchestrationId,
		"expected_revision": reread.View.Orchestration.Revision,
		"plan_record_id":    "pkw:plan/smoke/nonexistent",
	})
	if !strings.Contains(msg, "no knowledge record") {
		t.Fatalf("refusal = %q, want it to say the plan record does not exist", msg)
	}

	// A call that asks for nothing is refused rather than recorded as an
	// event that changes nothing but invalidates everyone's revision.
	if msg := callToolExpectingError(t, cs, "update_delivery", map[string]any{
		"orchestration_id":  started.OrchestrationId,
		"expected_revision": reread.View.Orchestration.Revision,
	}); !strings.Contains(msg, "at least one field") {
		t.Fatalf("refusal = %q, want it to ask for a field to change", msg)
	}
}

// TestUpdateDeliveryAttachesAndDetachesProjects covers project
// membership over the wire, including the refusal that keeps a project
// with unfinished lanes attached.
func TestUpdateDeliveryAttachesAndDetachesProjects(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)
	ctx := context.Background()

	var started StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{"references": []string{"PAY-1842"}}, &started)

	var project RegisterProjectOutput
	callTool(t, cs, "register_project", map[string]any{
		"slug":           "payments",
		"repository_url": "https://example.test/payments.git",
		"default_branch": "main",
	}, &project)

	var attached DeliveryViewOutput
	callTool(t, cs, "update_delivery", map[string]any{
		"orchestration_id":   started.OrchestrationId,
		"expected_revision":  started.View.Orchestration.Revision,
		"attach_project_ids": []string{project.Project.Id},
	}, &attached)
	if len(attached.View.Projects) != 1 || !attached.View.Projects[0].Attached {
		t.Fatalf("view.projects = %+v, want the attached project listed even with no lanes", attached.View.Projects)
	}

	// A lane in that project makes it undetachable until the lane
	// finishes; nothing about the lane is changed by the refusal.
	var lane CreateLaneOutput
	callTool(t, cs, "create_lane", map[string]any{
		"orchestration_id": started.OrchestrationId,
		"project_id":       project.Project.Id,
		"session_id":       "pkw:run/smoke/adhoc-17",
	}, &lane)
	if lane.Lane.SessionId == nil || *lane.Lane.SessionId != "pkw:run/smoke/adhoc-17" {
		t.Fatalf("lane.session_id = %v, want the session that opened it", lane.Lane.SessionId)
	}

	msg := callToolExpectingError(t, cs, "update_delivery", map[string]any{
		"orchestration_id":   started.OrchestrationId,
		"expected_revision":  attached.View.Orchestration.Revision,
		"detach_project_ids": []string{project.Project.Id},
	})
	if !strings.Contains(msg, "terminal status") {
		t.Fatalf("refusal = %q, want it to explain the unfinished lanes", msg)
	}

	var stillThere DeliveryViewOutput
	callTool(t, cs, "get_delivery", map[string]any{"orchestration_id": started.OrchestrationId}, &stillThere)
	if len(stillThere.View.Lanes) != 1 || stillThere.View.Lanes[0].LaneID != lane.Lane.Id {
		t.Fatalf("view.lanes = %+v, want the lane untouched by the refused detach", stillThere.View.Lanes)
	}
	if stillThere.View.Lanes[0].SessionID != "pkw:run/smoke/adhoc-17" {
		t.Fatalf("lane session on the view = %q, want it surfaced", stillThere.View.Lanes[0].SessionID)
	}

	// Finish the lane through the delivery store - no MCP tool sets a
	// lane straight to accepted - and the detach then succeeds.
	db, err := a.OpenStorage(ctx)
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	store := delivery.NewStore(db)
	if _, err := store.UpdateLaneStatus(ctx, delivery.NewID(), started.OrchestrationId, lane.Lane.Id, lane.Lane.Revision, protocol.DeliveryLaneStatusAccepted); err != nil {
		t.Fatalf("UpdateLaneStatus: %v", err)
	}

	var current DeliveryViewOutput
	callTool(t, cs, "get_delivery", map[string]any{"orchestration_id": started.OrchestrationId}, &current)

	var detached DeliveryViewOutput
	callTool(t, cs, "update_delivery", map[string]any{
		"orchestration_id":   started.OrchestrationId,
		"expected_revision":  current.View.Orchestration.Revision,
		"detach_project_ids": []string{project.Project.Id},
	}, &detached)
	if len(detached.View.Projects) != 1 {
		t.Fatalf("view.projects = %+v, want the project still reported for its finished lane", detached.View.Projects)
	}
	if detached.View.Projects[0].Attached {
		t.Fatalf("view.projects[0] = %+v, want attached false after detaching", detached.View.Projects[0])
	}
}
