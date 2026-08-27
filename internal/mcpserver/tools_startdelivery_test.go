package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// TestStartDeliveryGetDeliveryAnswerCancelEndToEnd drives the four
// delivery-facade tools over the real MCP wire protocol end to end:
// start_delivery bootstraps an orchestration with one ambiguous
// reference among clear ones, get_delivery reads it back,
// answer_delivery_question resolves the ambiguous one (both the
// resolved-requirement and routing cases), and cancel_delivery ends
// the orchestration.
func TestStartDeliveryGetDeliveryAnswerCancelEndToEnd(t *testing.T) {
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
		t.Fatalf("Lanes = %+v, want none yet - this call passed no projects, so it creates no parent tasks or lanes", started.View.Lanes)
	}

	var got DeliveryViewOutput
	callTool(t, cs, "get_delivery", map[string]any{"orchestration_id": started.OrchestrationId}, &got)
	if got.View.Orchestration.Id != started.OrchestrationId {
		t.Fatalf("get_delivery returned a different orchestration: %+v", got.View.Orchestration)
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

// TestStartDeliveryWithoutProjectsCreatesNoGraph pins the backward
// compatibility contract: a call that omits projects behaves exactly as
// it did before that field existed - requirements captured, no project
// registered, no parent task, no lane, and nothing reported under
// decomposition.
func TestStartDeliveryWithoutProjectsCreatesNoGraph(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)
	ctx := context.Background()

	var started StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"references": []string{"PAY-2001", "https://example.com/no-projects"},
	}, &started)

	if len(started.Decomposition) != 0 {
		t.Fatalf("Decomposition = %+v, want none when projects is omitted", started.Decomposition)
	}
	if len(started.View.Lanes) != 0 || len(started.View.Projects) != 0 {
		t.Fatalf("Lanes = %+v, Projects = %+v, want both empty when projects is omitted", started.View.Lanes, started.View.Projects)
	}

	db, err := a.OpenStorage(ctx)
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	store := delivery.NewStore(db)
	tasks, _, err := store.ListGraph(ctx, started.OrchestrationId)
	if err != nil {
		t.Fatalf("ListGraph: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("ListGraph tasks = %+v, want none when projects is omitted", tasks)
	}
	sources, err := store.ListRequirementSources(ctx, started.OrchestrationId)
	if err != nil {
		t.Fatalf("ListRequirementSources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("ListRequirementSources = %d sources, want the 2 classified references still captured", len(sources))
	}
}

// TestStartDeliveryWithProjectsCreatesLanesInOneCall covers the whole
// point of the projects field: one call has to leave a delivery that can
// actually run, not a pending shell the caller must decompose over three
// more round trips.
func TestStartDeliveryWithProjectsCreatesLanesInOneCall(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)
	ctx := context.Background()

	var started StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"references": []string{"PAY-3001", "PAY-3002"},
		"projects": []map[string]any{{
			"slug":           "checkout-api",
			"repository_url": "https://example.test/checkout-api.git",
			"default_branch": "main",
			"tasks": []map[string]any{
				{"title": "expose the new charge endpoint", "references": []string{"PAY-3001"}},
				{"title": "migrate the settlement job", "references": []string{"PAY-3002"}},
			},
		}},
	}, &started)

	if len(started.Decomposition) != 1 {
		t.Fatalf("Decomposition = %+v, want exactly the one requested project", started.Decomposition)
	}
	got := started.Decomposition[0]
	if got.Skipped != "" {
		t.Fatalf("Skipped = %q, want nothing skipped", got.Skipped)
	}
	if got.ProjectId == "" || got.Slug != "checkout-api" {
		t.Fatalf("Decomposition[0] = %+v, want the registered checkout-api project", got)
	}
	if len(got.ParentTaskIds) != 2 || len(got.LaneIds) != 2 {
		t.Fatalf("ParentTaskIds = %v, LaneIds = %v, want two of each", got.ParentTaskIds, got.LaneIds)
	}
	if len(started.View.Lanes) != 2 {
		t.Fatalf("View.Lanes = %+v, want the 2 lanes this call just created, not the pre-decomposition empty view", started.View.Lanes)
	}
	for _, lane := range started.View.Lanes {
		if lane.ProjectID != got.ProjectId {
			t.Fatalf("lane %s belongs to project %s, want %s", lane.LaneID, lane.ProjectID, got.ProjectId)
		}
		if lane.ParentTaskID == "" {
			t.Fatalf("lane %s has no parent task; each task's lane must be wired to it", lane.LaneID)
		}
	}

	// Each parent task must group only the source its own reference named,
	// not every captured source.
	db, err := a.OpenStorage(ctx)
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	store := delivery.NewStore(db)
	sources, err := store.ListRequirementSources(ctx, started.OrchestrationId)
	if err != nil {
		t.Fatalf("ListRequirementSources: %v", err)
	}
	idByExternal := map[string]string{}
	for _, src := range sources {
		if src.ExternalId != nil {
			idByExternal[*src.ExternalId] = src.Id
		}
	}
	wantSourceByTitle := map[string]string{
		"expose the new charge endpoint": idByExternal["PAY-3001"],
		"migrate the settlement job":     idByExternal["PAY-3002"],
	}
	tasks, _, err := store.ListGraph(ctx, started.OrchestrationId)
	if err != nil {
		t.Fatalf("ListGraph: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("ListGraph tasks = %+v, want two", tasks)
	}
	for _, task := range tasks {
		want, known := wantSourceByTitle[task.Title]
		if !known {
			t.Fatalf("unexpected parent task title %q", task.Title)
		}
		if len(task.SourceIds) != 1 || task.SourceIds[0] != want {
			t.Fatalf("task %q SourceIds = %v, want exactly [%s]", task.Title, task.SourceIds, want)
		}
	}
}

// TestStartDeliveryProjectsSkipsUnmatchedTaskReferences covers the
// partial-failure policy: one bad task reference must cost that one task
// and be explained, never the whole delivery.
func TestStartDeliveryProjectsSkipsUnmatchedTaskReferences(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	var started StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"references": []string{"PAY-4001"},
		"projects": []map[string]any{{
			"slug":           "billing-worker",
			"repository_url": "https://example.test/billing-worker.git",
			"tasks": []map[string]any{
				{"title": "real work", "references": []string{"PAY-4001"}},
				{"title": "phantom work", "references": []string{"PAY-9999"}},
			},
		}},
	}, &started)

	if len(started.Decomposition) != 1 {
		t.Fatalf("Decomposition = %+v, want exactly the one requested project", started.Decomposition)
	}
	got := started.Decomposition[0]
	if got.ProjectId == "" {
		t.Fatalf("Decomposition[0] = %+v, want the project registered despite the bad task", got)
	}
	if len(got.ParentTaskIds) != 1 || len(got.LaneIds) != 1 {
		t.Fatalf("ParentTaskIds = %v, LaneIds = %v, want only the matched task's task and lane", got.ParentTaskIds, got.LaneIds)
	}
	if !strings.Contains(got.Skipped, "phantom work") || !strings.Contains(got.Skipped, "PAY-9999") {
		t.Fatalf("Skipped = %q, want it to name the skipped task and the reference that matched nothing", got.Skipped)
	}
	if strings.Contains(got.Skipped, "real work") {
		t.Fatalf("Skipped = %q, want the matched task left out of it", got.Skipped)
	}
	if len(started.View.Lanes) != 1 {
		t.Fatalf("View.Lanes = %+v, want the one lane that could be created", started.View.Lanes)
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

// TestStartDeliveryAcceptsTitleAndDerivesOneOtherwise covers the tool
// surface's half of delivery titles: a supplied title comes back verbatim
// beside the orchestration id, and an omitted one comes back derived from
// the call's own references rather than blank - so nothing a caller shows
// a human is ever just 26 opaque characters.
func TestStartDeliveryAcceptsTitleAndDerivesOneOtherwise(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	var supplied StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"references": []string{"PAY-1842", "acme/checkout#42"},
		"title":      "migrate checkout to the new payments API",
	}, &supplied)
	if supplied.Title != "migrate checkout to the new payments API" {
		t.Fatalf("Title = %q, want the supplied title", supplied.Title)
	}
	if supplied.View.Title != supplied.Title {
		t.Fatalf("view.title = %q, want it to match the output's title %q", supplied.View.Title, supplied.Title)
	}

	var derived StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"references": []string{"PAY-1842", "acme/checkout#42"},
	}, &derived)
	if derived.Title != "PAY-1842 (+1 more)" {
		t.Fatalf("Title = %q, want one derived from the references", derived.Title)
	}

	// get_delivery must report the same label, so a caller resuming an
	// orchestration it did not start sees what the work is for too.
	var got DeliveryViewOutput
	callTool(t, cs, "get_delivery", map[string]any{"orchestration_id": derived.OrchestrationId}, &got)
	if got.View.Title != derived.Title {
		t.Fatalf("get_delivery view.title = %q, want %q", got.View.Title, derived.Title)
	}
}
