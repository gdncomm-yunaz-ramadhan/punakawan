package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// TestStartDeliveryGetDeliveryAnswerApproveCancelEndToEnd drives the six
// delivery-facade tools over the real MCP wire protocol end to end:
// start_delivery bootstraps an orchestration with one ambiguous
// reference among clear ones, get_delivery reads it back,
// answer_delivery_question resolves the ambiguous one, a directly
// seeded pending approval manifest is decided via
// approve_project_delivery (covering both the reject and approve
// paths), and cancel_delivery ends the orchestration.
func TestStartDeliveryGetDeliveryAnswerApproveCancelEndToEnd(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)
	ctx := context.Background()

	var started StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"references": []string{"PAY-1842", "https://example.com/spec", "an ambiguous free text note"},
	}, &started)

	if started.OrchestrationId == "" {
		t.Fatal("start_delivery returned an empty orchestration id")
	}
	if len(started.View.PendingQuestions) != 1 || started.View.PendingQuestions[0] != "an ambiguous free text note" {
		t.Fatalf("PendingQuestions = %+v, want exactly the one unclassifiable reference", started.View.PendingQuestions)
	}
	if !strings.Contains(started.View.NextAction, "answer_delivery_question") {
		t.Fatalf("NextAction = %q, want it to mention answer_delivery_question", started.View.NextAction)
	}
	if len(started.View.Lanes) != 0 {
		t.Fatalf("Lanes = %+v, want none yet - start_delivery never creates parent tasks or lanes", started.View.Lanes)
	}

	var got DeliveryViewOutput
	callTool(t, cs, "get_delivery", map[string]any{"orchestration_id": started.OrchestrationId}, &got)
	if got.View.Orchestration.Id != started.OrchestrationId {
		t.Fatalf("get_delivery returned a different orchestration: %+v", got.View.Orchestration)
	}

	var resumed DeliveryViewOutput
	callTool(t, cs, "resume_delivery", map[string]any{"orchestration_id": started.OrchestrationId}, &resumed)
	if resumed.View.Orchestration.Revision != got.View.Orchestration.Revision {
		t.Fatalf("resume_delivery = %+v, want the same state get_delivery just reported", resumed.View.Orchestration)
	}

	var answered DeliveryViewOutput
	callTool(t, cs, "answer_delivery_question", map[string]any{
		"orchestration_id":  started.OrchestrationId,
		"reference":         "an ambiguous free text note",
		"expected_revision": got.View.Orchestration.Revision,
		"provider":          "freetext",
		"title":             "resolved note",
		"summary":           "turned out to be about the checkout flow",
	}, &answered)
	if len(answered.View.PendingQuestions) != 0 {
		t.Fatalf("PendingQuestions = %+v, want none left after answering the only one", answered.View.PendingQuestions)
	}

	// answer_delivery_question's routing case, exercised against a
	// separately seeded, still-unrouted parent task: no MCP tool creates
	// parent tasks yet (tools_delivery_test.go's seedRunnableLane doc
	// comment notes the same gap for lanes/tasks/orchestrations), so this
	// seeds one directly through the delivery package, exactly like that
	// existing test file does.
	db, err := a.OpenStorage(ctx)
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	store := delivery.NewStore(db)
	proj, err := store.RegisterProject(ctx, "proj-"+delivery.NewID(), delivery.NewID(), "start-delivery-e2e", "https://example.test/start-delivery-e2e.git", "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	source, err := store.CaptureRequirement(ctx, "cap-"+delivery.NewID(), started.OrchestrationId, delivery.SourceInput{Provider: "jira", ExternalID: "PAY-9001", Title: "ambiguously routed"})
	if err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
	unroutedTask, err := store.CreateParentTask(ctx, "task-"+delivery.NewID(), delivery.NewID(), started.OrchestrationId, "ambiguously routed task", []string{source.Id})
	if err != nil {
		t.Fatalf("CreateParentTask: %v", err)
	}

	var routed DeliveryViewOutput
	callTool(t, cs, "answer_delivery_question", map[string]any{
		"orchestration_id": started.OrchestrationId,
		"reference":        "which project owns the ambiguously routed task",
		"parent_task_id":   unroutedTask.Id,
		"project_id":       proj.Id,
	}, &routed)

	routedTask, err := store.GetParentTask(ctx, started.OrchestrationId, unroutedTask.Id)
	if err != nil {
		t.Fatalf("GetParentTask: %v", err)
	}
	if routedTask.ProjectId == nil || *routedTask.ProjectId != proj.Id {
		t.Fatalf("task ProjectId = %v, want %s after routing via answer_delivery_question", routedTask.ProjectId, proj.Id)
	}

	// Seed one pending approval manifest directly (create_approval_manifest
	// has no MCP tool either) to exercise approve_project_delivery's reject
	// path.
	rejectManifest, err := store.CreateApprovalManifest(ctx, "manifest-reject-"+delivery.NewID(), delivery.NewID(), started.OrchestrationId, proj.Id, []string{routedTask.Id}, delivery.ManifestPlan{PlannedBaseRef: "main"})
	if err != nil {
		t.Fatalf("CreateApprovalManifest(reject): %v", err)
	}

	var rejected DeliveryViewOutput
	callTool(t, cs, "approve_project_delivery", map[string]any{
		"orchestration_id": started.OrchestrationId,
		"manifest_id":      rejectManifest.Id,
		"approved_by":      "a-human-reviewer",
		"reject":           true,
	}, &rejected)
	for _, m := range rejected.View.PendingApprovals {
		if m.Id == rejectManifest.Id {
			t.Fatalf("manifest %s still reported pending after reject: %+v", rejectManifest.Id, m)
		}
	}

	// A second manifest, this time approved.
	approveManifest, err := store.CreateApprovalManifest(ctx, "manifest-approve-"+delivery.NewID(), delivery.NewID(), started.OrchestrationId, proj.Id, []string{routedTask.Id}, delivery.ManifestPlan{PlannedBaseRef: "main"})
	if err != nil {
		t.Fatalf("CreateApprovalManifest(approve): %v", err)
	}

	var approved DeliveryViewOutput
	callTool(t, cs, "approve_project_delivery", map[string]any{
		"orchestration_id": started.OrchestrationId,
		"manifest_id":      approveManifest.Id,
		"approved_by":      "a-human-reviewer",
	}, &approved)
	for _, m := range approved.View.PendingApprovals {
		if m.Id == approveManifest.Id {
			t.Fatalf("manifest %s still reported pending after approve: %+v", approveManifest.Id, m)
		}
	}
	if strings.Contains(approved.View.NextAction, string(rejectManifest.Id)) {
		t.Fatalf("NextAction unexpectedly references the already-decided reject manifest: %q", approved.View.NextAction)
	}

	var cancelled DeliveryViewOutput
	callTool(t, cs, "cancel_delivery", map[string]any{
		"orchestration_id":  started.OrchestrationId,
		"expected_revision": approved.View.Orchestration.Revision,
		"reason":            "end to end test cleanup",
	}, &cancelled)
	if cancelled.View.Orchestration.Status != protocol.DeliveryOrchestrationStatusCancelled {
		t.Fatalf("Orchestration.Status = %s, want cancelled", cancelled.View.Orchestration.Status)
	}
}

// TestStartDeliveryRetryWithSameIdempotencyKeyReturnsSameOrchestration
// covers start_delivery's own idempotency contract over the MCP wire: a
// client retrying the same call (e.g. after a dropped response) must
// see the same orchestration, not a second one.
func TestStartDeliveryRetryWithSameIdempotencyKeyReturnsSameOrchestration(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	var first StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"references":      []string{"PAY-1"},
		"idempotency_key": "retry-key-mcp",
	}, &first)

	var second StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"references":      []string{"PAY-1"},
		"idempotency_key": "retry-key-mcp",
	}, &second)

	if first.OrchestrationId != second.OrchestrationId {
		t.Fatalf("retry minted a different orchestration: first=%s second=%s", first.OrchestrationId, second.OrchestrationId)
	}
}

// TestAnswerDeliveryQuestionRequiresEitherCase covers the input-validation
// branch: neither the resolved-requirement fields nor the routing fields
// set must fail clearly, not silently no-op.
func TestAnswerDeliveryQuestionRequiresEitherCase(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	var started StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{"references": []string{"an ambiguous note"}}, &started)

	ctx := context.Background()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "answer_delivery_question", Arguments: map[string]any{
		"orchestration_id": started.OrchestrationId,
		"reference":        "an ambiguous note",
	}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result when neither case's fields are set")
	}
}
