package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// TestRegisterProjectCreateParentTaskAddDependencyEdgeCreateLaneEndToEnd
// drives the full up-front graph-authoring path over the real MCP wire
// protocol: start_delivery bootstraps an orchestration,
// register_project registers a target project, create_parent_task
// groups requirement sources into two tasks, answer_delivery_question's
// routing case assigns both to the project (no standalone
// route_parent_task tool exists - that is answer_delivery_question's
// job per the delivery-facade design, not this scope),
// add_dependency_edge records an explicit dependency between them and
// separately verifies the graph's cycle guard still rejects a reverse
// edge, and create_lane creates a lane for the routed, unblocked task -
// which list_runnable_lanes (triggering a frontier sync) and then
// get_delivery both then report as runnable.
func TestRegisterProjectCreateParentTaskAddDependencyEdgeCreateLaneEndToEnd(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)
	ctx := context.Background()

	var started StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"references": []string{"PAY-1"},
	}, &started)

	// create_parent_task requires already-captured requirement sources;
	// no MCP tool captures one on its own (that's start_delivery's or
	// answer_delivery_question's job), so two more are seeded directly
	// through the delivery package, exactly like
	// tools_startdelivery_test.go's end-to-end test does.
	db, err := a.OpenStorage(ctx)
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	store := delivery.NewStore(db)
	sourceA, err := store.CaptureRequirement(ctx, "cap-a-"+delivery.NewID(), started.OrchestrationId, delivery.SourceInput{Provider: "jira", ExternalID: "GRAPH-1", Title: "task A requirement"})
	if err != nil {
		t.Fatalf("CaptureRequirement A: %v", err)
	}
	sourceB, err := store.CaptureRequirement(ctx, "cap-b-"+delivery.NewID(), started.OrchestrationId, delivery.SourceInput{Provider: "jira", ExternalID: "GRAPH-2", Title: "task B requirement"})
	if err != nil {
		t.Fatalf("CaptureRequirement B: %v", err)
	}

	var registered RegisterProjectOutput
	callTool(t, cs, "register_project", map[string]any{
		"slug":           "taskgraph-e2e",
		"repository_url": "https://example.test/taskgraph-e2e.git",
		"default_branch": "main",
	}, &registered)
	if registered.Project.Id == "" {
		t.Fatal("register_project returned an empty project id")
	}
	if registered.Project.Slug != "taskgraph-e2e" {
		t.Fatalf("Project.Slug = %q, want taskgraph-e2e", registered.Project.Slug)
	}

	var taskA CreateParentTaskOutput
	callTool(t, cs, "create_parent_task", map[string]any{
		"orchestration_id": started.OrchestrationId,
		"title":            "task A",
		"source_ids":       []string{sourceA.Id},
	}, &taskA)
	if taskA.ParentTask.Id == "" {
		t.Fatal("create_parent_task (A) returned an empty task id")
	}
	if taskA.View.Orchestration.Id != started.OrchestrationId {
		t.Fatalf("create_parent_task (A) view orchestration = %s, want %s", taskA.View.Orchestration.Id, started.OrchestrationId)
	}

	var taskB CreateParentTaskOutput
	callTool(t, cs, "create_parent_task", map[string]any{
		"orchestration_id": started.OrchestrationId,
		"title":            "task B",
		"source_ids":       []string{sourceB.Id},
	}, &taskB)

	// Route both tasks to the registered project via
	// answer_delivery_question's routing case - the only existing tool
	// that calls RouteParentTask.
	var routedA DeliveryViewOutput
	callTool(t, cs, "answer_delivery_question", map[string]any{
		"orchestration_id": started.OrchestrationId,
		"reference":        "which project owns task A",
		"parent_task_id":   taskA.ParentTask.Id,
		"project_id":       registered.Project.Id,
	}, &routedA)

	var routedB DeliveryViewOutput
	callTool(t, cs, "answer_delivery_question", map[string]any{
		"orchestration_id": started.OrchestrationId,
		"reference":        "which project owns task B",
		"parent_task_id":   taskB.ParentTask.Id,
		"project_id":       registered.Project.Id,
	}, &routedB)

	// B depends on A: A has no predecessor and stays on the frontier,
	// B does not until A is resolved.
	var edge AddDependencyEdgeOutput
	callTool(t, cs, "add_dependency_edge", map[string]any{
		"orchestration_id": started.OrchestrationId,
		"from_task_id":     taskB.ParentTask.Id,
		"to_task_id":       taskA.ParentTask.Id,
		"edge_type":        "requires",
		"evidence":         "task B consumes task A's output",
	}, &edge)
	if edge.Edge.FromTaskId != taskB.ParentTask.Id || edge.Edge.ToTaskId != taskA.ParentTask.Id {
		t.Fatalf("unexpected edge recorded: %+v", edge.Edge)
	}
	if edge.Edge.Origin != protocol.DependencyEdgeOriginUser {
		t.Fatalf("Edge.Origin = %s, want user (explicitly authored, not discovered)", edge.Edge.Origin)
	}
	if edge.Edge.Confidence != 1.0 {
		t.Fatalf("Edge.Confidence = %v, want 1.0 default for an explicitly authored edge", edge.Edge.Confidence)
	}

	// The cycle guard (graph_test.go's TestCycleRejectedWithoutMutation
	// covers this directly against the Store; this repeats it through
	// the new tool) must still reject the reverse edge.
	cycleRes, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "add_dependency_edge", Arguments: map[string]any{
		"orchestration_id": started.OrchestrationId,
		"from_task_id":     taskA.ParentTask.Id,
		"to_task_id":       taskB.ParentTask.Id,
		"edge_type":        "requires",
	}})
	if err != nil {
		t.Fatalf("CallTool(add_dependency_edge, reverse): %v", err)
	}
	if !cycleRes.IsError {
		t.Fatal("expected add_dependency_edge to reject an edge that would close a cycle")
	}

	var lane CreateLaneOutput
	callTool(t, cs, "create_lane", map[string]any{
		"orchestration_id": started.OrchestrationId,
		"project_id":       registered.Project.Id,
		"parent_task_id":   taskA.ParentTask.Id,
	}, &lane)
	if lane.Lane.Id == "" {
		t.Fatal("create_lane returned an empty lane id")
	}
	if lane.Lane.ProjectId != registered.Project.Id {
		t.Fatalf("Lane.ProjectId = %s, want %s", lane.Lane.ProjectId, registered.Project.Id)
	}

	// list_runnable_lanes syncs the frontier; task A has no unresolved
	// predecessor so its lane should move to runnable.
	var runnable ListRunnableLanesOutput
	callTool(t, cs, "list_runnable_lanes", map[string]any{"orchestration_id": started.OrchestrationId}, &runnable)
	found := false
	for _, l := range runnable.Lanes {
		if l.Id == lane.Lane.Id {
			found = true
		}
	}
	if !found {
		t.Fatalf("list_runnable_lanes did not report lane %s as runnable: %+v", lane.Lane.Id, runnable.Lanes)
	}

	var view DeliveryViewOutput
	callTool(t, cs, "get_delivery", map[string]any{"orchestration_id": started.OrchestrationId}, &view)
	var laneSummary *delivery.LaneSummary
	for i := range view.View.Lanes {
		if view.View.Lanes[i].LaneID == lane.Lane.Id {
			laneSummary = &view.View.Lanes[i]
		}
	}
	if laneSummary == nil {
		t.Fatalf("get_delivery view does not list lane %s: %+v", lane.Lane.Id, view.View.Lanes)
	}
	if laneSummary.Status != protocol.DeliveryLaneStatusRunnable {
		t.Fatalf("get_delivery lane status = %s, want runnable", laneSummary.Status)
	}
}

// TestRegisterProjectDuplicateSlugFails covers register_project's
// dedup behaviour over the MCP wire: RegisterProject itself fails a
// duplicate slug (a duplicate idempotency key is the only case it
// treats as harmless, and register_project never exposes one to the
// caller), so a second registration under the same slug must fail
// rather than silently succeed or return a second project.
func TestRegisterProjectDuplicateSlugFails(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)
	ctx := context.Background()

	var first RegisterProjectOutput
	callTool(t, cs, "register_project", map[string]any{
		"slug":           "dup-slug-e2e",
		"repository_url": "https://example.test/dup-slug-e2e.git",
		"default_branch": "main",
	}, &first)
	if first.Project.Id == "" {
		t.Fatal("first register_project returned an empty project id")
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "register_project", Arguments: map[string]any{
		"slug":           "dup-slug-e2e",
		"repository_url": "https://example.test/dup-slug-e2e-again.git",
		"default_branch": "main",
	}})
	if err != nil {
		t.Fatalf("CallTool(register_project, duplicate slug): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected register_project to fail on a duplicate slug")
	}

	// The first project must still be exactly as registered - a failed
	// duplicate attempt must not have mutated it.
	db, err := a.OpenStorage(ctx)
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	store := delivery.NewStore(db)
	reloaded, err := store.GetProject(ctx, first.Project.Id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if reloaded.RepositoryUrl != "https://example.test/dup-slug-e2e.git" {
		t.Fatalf("RepositoryUrl = %q, want the first registration's own URL untouched", reloaded.RepositoryUrl)
	}
}
