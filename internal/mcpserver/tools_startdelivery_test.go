package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// jiraSource is the identity every delivery in this file starts from.
func jiraSource(key string) map[string]any {
	return map[string]any{"kind": "jira", "tenant": "test-tenant", "key": key}
}

// TestStartDeliveryReconcilesProjectsPlansAndSessionInOneCall is the
// contract the whole tool exists for: one call must leave a delivery that
// can actually run and actually be measured - real lanes wired to real
// parent tasks, a session to attach usage to, and the two ids
// (execution, requirement source) that map_delivery_work_item needs and
// no other call hands out. It also pins the honesty half: a task naming
// something this delivery never captured costs that one task and says so,
// rather than disappearing.
func TestStartDeliveryReconcilesProjectsPlansAndSessionInOneCall(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)
	ctx := context.Background()

	var started StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"source":      jiraSource("PAY-3001"),
		"title":       "migrate checkout to the new payments API",
		"description": "raise the charge endpoint onto v2",
		"projects": []map[string]any{{
			"slug":           "checkout-api",
			"repository_url": "https://example.test/checkout-api.git",
			"default_branch": "main",
			"tasks": []map[string]any{
				{"title": "expose the new charge endpoint"},
				{"title": "phantom work", "references": []string{"PAY-9999"}},
			},
		}},
	}, &started)

	if started.Status != "started" {
		t.Fatalf("Status = %q, want started", started.Status)
	}
	if started.OrchestrationId == "" || started.ExecutionId == "" {
		t.Fatalf("OrchestrationId = %q, ExecutionId = %q, want both - map_delivery_work_item needs the execution id and nothing else reports it", started.OrchestrationId, started.ExecutionId)
	}
	if started.Session == nil || started.Session.ID == "" {
		t.Fatal("Session is empty: a delivery with no session records no usage at all, so one must be opened even when the caller describes none")
	}
	if started.Title != "migrate checkout to the new payments API" || started.View.Title != started.Title {
		t.Fatalf("Title = %q, view.title = %q, want the supplied title in both", started.Title, started.View.Title)
	}

	if len(started.RequirementSources) != 1 || started.RequirementSources[0].ExternalId != "PAY-3001" {
		t.Fatalf("RequirementSources = %+v, want the delivery's own Jira parent captured", started.RequirementSources)
	}
	if started.RequirementSources[0].Id == "" {
		t.Fatal("captured requirement source has no id; map_delivery_work_item cannot be called without it")
	}

	if len(started.Reconciliation.Projects) != 1 {
		t.Fatalf("Reconciliation.Projects = %v, want exactly the one requested project", started.Reconciliation.Projects)
	}
	if len(started.View.Lanes) != 1 {
		t.Fatalf("View.Lanes = %+v, want the one lane this call just created, not a pre-reconciliation empty view", started.View.Lanes)
	}
	lane := started.View.Lanes[0]
	if lane.ParentTaskID == "" || lane.ProjectID != started.Reconciliation.Projects[0] {
		t.Fatalf("lane = %+v, want it wired to a parent task inside the reconciled project", lane)
	}

	skipped := strings.Join(started.Reconciliation.Skipped, "; ")
	if !strings.Contains(skipped, "phantom work") || !strings.Contains(skipped, "PAY-9999") {
		t.Fatalf("Reconciliation.Skipped = %q, want it to name the task that was dropped and the key that matched nothing", skipped)
	}
	if strings.Contains(skipped, "expose the new charge endpoint") {
		t.Fatalf("Reconciliation.Skipped = %q, want the task that succeeded left out of it", skipped)
	}

	// The parent task must group only the source it covers.
	db, err := a.OpenStorage(ctx)
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	store := delivery.NewStore(db)
	tasks, _, err := store.ListGraph(ctx, started.OrchestrationId)
	if err != nil {
		t.Fatalf("ListGraph: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("ListGraph tasks = %+v, want only the one that matched", tasks)
	}
	if len(tasks[0].SourceIds) != 1 || tasks[0].SourceIds[0] != started.RequirementSources[0].Id {
		t.Fatalf("task SourceIds = %v, want exactly the captured parent source", tasks[0].SourceIds)
	}
	if tasks[0].ProjectId == nil || *tasks[0].ProjectId != lane.ProjectID {
		t.Fatalf("task ProjectId = %v, want it routed to the lane's project %s", tasks[0].ProjectId, lane.ProjectID)
	}
}

// TestStartDeliveryReusesOneLifetimePerJiraKey covers both halves of
// delivery identity: a retried call resolves to the delivery it already
// started, and a later call that discovered more work reconciles it onto
// that same delivery rather than minting a second one for the same Jira
// parent.
func TestStartDeliveryReusesOneLifetimePerJiraKey(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	var first StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"source":          jiraSource("PAY-1"),
		"idempotency_key": "retry-key-mcp",
	}, &first)
	if len(first.View.Lanes) != 0 {
		t.Fatalf("View.Lanes = %+v, want none when the call names no projects", first.View.Lanes)
	}

	var retried StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"source":          jiraSource("PAY-1"),
		"idempotency_key": "retry-key-mcp",
	}, &retried)
	if retried.OrchestrationId != first.OrchestrationId {
		t.Fatalf("retry minted a different orchestration: first=%s retried=%s", first.OrchestrationId, retried.OrchestrationId)
	}

	var extended StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"source": jiraSource("PAY-1"),
		"projects": []map[string]any{{
			"slug":           "billing-worker",
			"repository_url": "https://example.test/billing-worker.git",
			"tasks":          []map[string]any{{"title": "work discovered later"}},
		}},
	}, &extended)
	if extended.OrchestrationId != first.OrchestrationId {
		t.Fatalf("a second call for the same Jira key started a new delivery %s, want the existing %s", extended.OrchestrationId, first.OrchestrationId)
	}
	if len(extended.View.Lanes) != 1 {
		t.Fatalf("View.Lanes = %+v, want the lane reconciled onto the existing delivery", extended.View.Lanes)
	}
}

// TestStartDeliveryRequiresASource pins the one input that cannot be
// defaulted: without a source there is no identity to reuse a lifetime by,
// and the caller must be told so rather than handed an orphan delivery.
func TestStartDeliveryRequiresASource(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	var started StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{"title": "no source at all"}, &started)
	if started.Status != "needs_input" || started.NeedsInput == nil {
		t.Fatalf("Status = %q, NeedsInput = %+v, want a needs_input result", started.Status, started.NeedsInput)
	}
	if started.OrchestrationId != "" {
		t.Fatalf("OrchestrationId = %q, want nothing written when the source is missing", started.OrchestrationId)
	}
}

// TestGetDeliveryAnswerRoutingAndCancel drives the remaining facade tools
// over the real MCP wire: reading a delivery back, routing an unrouted
// parent task through answer_delivery_question, rejecting a call that sets
// neither of that tool's two cases, and cancelling.
func TestGetDeliveryAnswerRoutingAndCancel(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)
	ctx := context.Background()

	var started StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{"source": jiraSource("PAY-42")}, &started)

	var got DeliveryViewOutput
	callTool(t, cs, "get_delivery", map[string]any{"orchestration_id": started.OrchestrationId}, &got)
	if got.View.Orchestration.Id != started.OrchestrationId {
		t.Fatalf("get_delivery returned a different orchestration: %+v", got.View.Orchestration)
	}
	if got.View.Title != started.Title {
		t.Fatalf("get_delivery view.title = %q, want %q", got.View.Title, started.Title)
	}

	// No tool creates an unrouted parent task, so one is seeded directly
	// to exercise answer_delivery_question's routing case.
	db, err := a.OpenStorage(ctx)
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	store := delivery.NewStore(db)
	proj, err := store.RegisterProject(ctx, "proj-"+delivery.NewID(), delivery.NewID(), "routing-target", "https://example.test/routing-target.git", "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	sources, err := store.ListRequirementSources(ctx, started.OrchestrationId)
	if err != nil || len(sources) == 0 {
		t.Fatalf("ListRequirementSources = %v, %v, want the captured Jira parent", sources, err)
	}
	unrouted, err := store.CreateParentTask(ctx, "task-"+delivery.NewID(), delivery.NewID(), started.OrchestrationId, "ambiguously routed task", []string{sources[0].Id})
	if err != nil {
		t.Fatalf("CreateParentTask: %v", err)
	}

	var routed DeliveryViewOutput
	callTool(t, cs, "answer_delivery_question", map[string]any{
		"orchestration_id": started.OrchestrationId,
		"reference":        "which project owns the ambiguously routed task",
		"parent_task_id":   unrouted.Id,
		"project_id":       proj.Id,
	}, &routed)
	routedTask, err := store.GetParentTask(ctx, started.OrchestrationId, unrouted.Id)
	if err != nil {
		t.Fatalf("GetParentTask: %v", err)
	}
	if routedTask.ProjectId == nil || *routedTask.ProjectId != proj.Id {
		t.Fatalf("task ProjectId = %v, want %s after routing", routedTask.ProjectId, proj.Id)
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "answer_delivery_question", Arguments: map[string]any{
		"orchestration_id": started.OrchestrationId,
		"reference":        "neither case",
	}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result when neither of answer_delivery_question's cases is set")
	}

	var cancelled DeliveryViewOutput
	callTool(t, cs, "cancel_delivery", map[string]any{
		"orchestration_id":  started.OrchestrationId,
		"expected_revision": routed.View.Orchestration.Revision,
		"reason":            "end to end test cleanup",
	}, &cancelled)
	if cancelled.View.Orchestration.Status != protocol.DeliveryOrchestrationStatusCancelled {
		t.Fatalf("Orchestration.Status = %s, want cancelled", cancelled.View.Orchestration.Status)
	}
}
