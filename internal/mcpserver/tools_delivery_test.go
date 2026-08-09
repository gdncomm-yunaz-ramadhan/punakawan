package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// seedRunnableLane creates an orchestration, project, task, and lane
// through the delivery package directly (no MCP tool creates these
// yet - that is separate, not-yet-built scope), returning the ids the
// MCP tools under test operate on.
func seedRunnableLane(t *testing.T, a *app.App) (orchestrationID, laneID string) {
	t.Helper()
	ctx := context.Background()
	db, err := a.OpenStorage(ctx)
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	store := delivery.NewStore(db)

	orch, err := store.CreateOrchestration(ctx, "orch-"+delivery.NewID(), delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	proj, err := store.RegisterProject(ctx, "proj-"+delivery.NewID(), delivery.NewID(), "delivery-mcp-test", "https://example.test/delivery-mcp-test.git", "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	source, err := store.CaptureRequirement(ctx, "cap-"+delivery.NewID(), orch.Id, delivery.SourceInput{Provider: "jira", ExternalID: "TEST-1", Title: "seed requirement"})
	if err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
	task, err := store.CreateParentTask(ctx, "task-"+delivery.NewID(), delivery.NewID(), orch.Id, "seed task", []string{source.Id})
	if err != nil {
		t.Fatalf("CreateParentTask: %v", err)
	}
	if _, err := store.RouteParentTask(ctx, "route-"+delivery.NewID(), orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	lane, err := store.CreateLane(ctx, "lane-"+delivery.NewID(), delivery.NewID(), orch.Id, proj.Id, task.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	return orch.Id, lane.Id
}

// TestDeliveryLeaseToolsEndToEnd drives claim/heartbeat/complete over
// the real MCP wire protocol, covering the pull-based path an external
// agent uses: list what's runnable, claim it, keep the lease alive,
// then report it done.
func TestDeliveryLeaseToolsEndToEnd(t *testing.T) {
	a := newTestApp(t)
	orchID, laneID := seedRunnableLane(t, a)
	cs := connect(t, a)

	var listOut ListRunnableLanesOutput
	callTool(t, cs, "list_runnable_lanes", map[string]any{"orchestration_id": orchID}, &listOut)
	if len(listOut.Lanes) != 1 || listOut.Lanes[0].Id != laneID {
		t.Fatalf("expected exactly the seeded lane runnable, got %+v", listOut.Lanes)
	}
	if listOut.Lanes[0].Status != protocol.DeliveryLaneStatusRunnable {
		t.Fatalf("expected runnable status, got %s", listOut.Lanes[0].Status)
	}

	var claimOut LaneOutput
	callTool(t, cs, "claim_lane", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"expected_revision": listOut.Lanes[0].Revision,
		"worker_id":         "agent-1",
	}, &claimOut)
	if claimOut.Lane.Status != protocol.DeliveryLaneStatusLeased {
		t.Fatalf("expected leased status after claim, got %s", claimOut.Lane.Status)
	}
	if claimOut.Lane.LeaseToken == nil || *claimOut.Lane.LeaseToken == "" {
		t.Fatal("expected a non-empty lease token")
	}
	leaseToken := *claimOut.Lane.LeaseToken

	var hbOut LaneOutput
	callTool(t, cs, "heartbeat_lease", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"lease_token":       leaseToken,
		"expected_revision": claimOut.Lane.Revision,
	}, &hbOut)
	if hbOut.Lane.Status != protocol.DeliveryLaneStatusRunning {
		t.Fatalf("expected running status after heartbeat, got %s", hbOut.Lane.Status)
	}

	var completeOut LaneOutput
	callTool(t, cs, "complete_lease", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"lease_token":       leaseToken,
		"expected_revision": hbOut.Lane.Revision,
	}, &completeOut)
	if completeOut.Lane.Status != protocol.DeliveryLaneStatusReview {
		t.Fatalf("expected review status after complete, got %s", completeOut.Lane.Status)
	}

	// The lane is no longer runnable, so a second listing must not
	// surface it again.
	var listAfter ListRunnableLanesOutput
	callTool(t, cs, "list_runnable_lanes", map[string]any{"orchestration_id": orchID}, &listAfter)
	if len(listAfter.Lanes) != 0 {
		t.Fatalf("expected no runnable lanes left, got %+v", listAfter.Lanes)
	}
}

// TestClaimLaneRejectsStaleRevision covers the concurrent-claim race: a
// second caller working from a stale revision must not also succeed.
func TestClaimLaneRejectsStaleRevision(t *testing.T) {
	a := newTestApp(t)
	orchID, laneID := seedRunnableLane(t, a)
	cs := connect(t, a)

	var listOut ListRunnableLanesOutput
	callTool(t, cs, "list_runnable_lanes", map[string]any{"orchestration_id": orchID}, &listOut)
	staleRevision := listOut.Lanes[0].Revision

	var claimOut LaneOutput
	callTool(t, cs, "claim_lane", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"expected_revision": staleRevision,
		"worker_id":         "agent-1",
	}, &claimOut)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "claim_lane", Arguments: map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"expected_revision": staleRevision,
		"worker_id":         "agent-2",
	}})
	if err != nil {
		t.Fatalf("CallTool(claim_lane): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a second claim against the same stale (already-leased) revision to fail")
	}
}
