package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// TestSyncFrontierExposesExactBlockersAndUnblocksOnCompletion checks
// that a task with an unresolved predecessor exposes exact blocker ids
// and consumes no lease, and that completing the last predecessor
// makes its dependent runnable.
func TestSyncFrontierExposesExactBlockersAndUnblocksOnCompletion(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "sched-project")

	blocker := createTestTask(t, s, orch.Id, "blocker")
	dependent := createTestTask(t, s, orch.Id, "dependent")
	if _, err := s.AddDependencyEdge(ctx, "edge-1", NewID(), orch.Id, dependent.Id, blocker.Id, protocol.DependencyEdgeTypeRequires, protocol.DependencyEdgeOriginUser, 1.0, ""); err != nil {
		t.Fatalf("AddDependencyEdge: %v", err)
	}
	if _, err := s.RouteParentTask(ctx, "route-1", orch.Id, blocker.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask(blocker): %v", err)
	}
	if _, err := s.RouteParentTask(ctx, "route-2", orch.Id, dependent.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask(dependent): %v", err)
	}
	blockerLane, err := s.CreateLane(ctx, "lane-1", NewID(), orch.Id, proj.Id, blocker.Id)
	if err != nil {
		t.Fatalf("CreateLane(blocker): %v", err)
	}
	dependentLane, err := s.CreateLane(ctx, "lane-2", NewID(), orch.Id, proj.Id, dependent.Id)
	if err != nil {
		t.Fatalf("CreateLane(dependent): %v", err)
	}

	lanes, err := s.SyncFrontier(ctx, "sync-1", orch.Id)
	if err != nil {
		t.Fatalf("SyncFrontier: %v", err)
	}
	byID := lanesByID(lanes)
	if byID[blockerLane.Id].Status != protocol.DeliveryLaneStatusRunnable {
		t.Fatalf("expected blocker lane runnable, got %s", byID[blockerLane.Id].Status)
	}
	if byID[dependentLane.Id].Status != protocol.DeliveryLaneStatusBlocked {
		t.Fatalf("expected dependent lane blocked, got %s", byID[dependentLane.Id].Status)
	}
	if got := byID[dependentLane.Id].BlockedBy; len(got) != 1 || got[0] != blocker.Id {
		t.Fatalf("expected exact blocker id [%s], got %v", blocker.Id, got)
	}

	// A blocked lane must never accept a lease.
	if _, err := s.GrantLease(ctx, "lease-blocked", orch.Id, dependentLane.Id, byID[dependentLane.Id].Revision, "worker-1", time.Minute); !errors.Is(err, ErrLaneNotRunnable) {
		t.Fatalf("expected ErrLaneNotRunnable for a blocked lane, got %v", err)
	}

	// Drive the blocker lane through its whole lifecycle to accepted.
	leased, err := s.GrantLease(ctx, "lease-1", orch.Id, blockerLane.Id, byID[blockerLane.Id].Revision, "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("GrantLease(blocker): %v", err)
	}
	completed, err := s.CompleteLease(ctx, "complete-1", orch.Id, blockerLane.Id, *leased.LeaseToken, leased.Revision)
	if err != nil {
		t.Fatalf("CompleteLease(blocker): %v", err)
	}
	if completed.Status != protocol.DeliveryLaneStatusReview {
		t.Fatalf("expected review status after CompleteLease, got %s", completed.Status)
	}
	accepted, err := s.UpdateLaneStatus(ctx, "accept-1", orch.Id, blockerLane.Id, completed.Revision, protocol.DeliveryLaneStatusAccepted)
	if err != nil {
		t.Fatalf("UpdateLaneStatus(accepted): %v", err)
	}
	if accepted.Status != protocol.DeliveryLaneStatusAccepted {
		t.Fatalf("expected accepted status, got %s", accepted.Status)
	}

	lanes, err = s.SyncFrontier(ctx, "sync-2", orch.Id)
	if err != nil {
		t.Fatalf("SyncFrontier after acceptance: %v", err)
	}
	byID = lanesByID(lanes)
	if byID[dependentLane.Id].Status != protocol.DeliveryLaneStatusRunnable {
		t.Fatalf("expected dependent lane runnable once its blocker is accepted, got %s", byID[dependentLane.Id].Status)
	}
	if len(byID[dependentLane.Id].BlockedBy) != 0 {
		t.Fatalf("expected no blockers left, got %v", byID[dependentLane.Id].BlockedBy)
	}
}

// TestGrantLeaseEnforcesOneMutatingLanePerProject checks the
// per-project concurrency limit: two runnable lanes in the same project
// cannot both be leased at once, but lanes in different projects can.
func TestGrantLeaseEnforcesOneMutatingLanePerProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	projA := registerProject(t, s, "proj-a")
	projB := registerProject(t, s, "proj-b")

	taskA1 := createTestTask(t, s, orch.Id, "a1")
	taskA2 := createTestTask(t, s, orch.Id, "a2")
	taskB1 := createTestTask(t, s, orch.Id, "b1")
	for _, task := range []*protocol.ParentTask{taskA1, taskA2} {
		if _, err := s.RouteParentTask(ctx, "route-"+task.Id, orch.Id, task.Id, projA.Id); err != nil {
			t.Fatalf("RouteParentTask: %v", err)
		}
	}
	if _, err := s.RouteParentTask(ctx, "route-"+taskB1.Id, orch.Id, taskB1.Id, projB.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}

	laneA1, err := s.CreateLane(ctx, "lane-a1", NewID(), orch.Id, projA.Id, taskA1.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	laneA2, err := s.CreateLane(ctx, "lane-a2", NewID(), orch.Id, projA.Id, taskA2.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	laneB1, err := s.CreateLane(ctx, "lane-b1", NewID(), orch.Id, projB.Id, taskB1.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	if _, err := s.SyncFrontier(ctx, "sync-1", orch.Id); err != nil {
		t.Fatalf("SyncFrontier: %v", err)
	}
	laneA1, _ = s.GetLane(ctx, orch.Id, laneA1.Id)
	laneA2, _ = s.GetLane(ctx, orch.Id, laneA2.Id)
	laneB1, _ = s.GetLane(ctx, orch.Id, laneB1.Id)

	if _, err := s.GrantLease(ctx, "lease-a1", orch.Id, laneA1.Id, laneA1.Revision, "worker-1", time.Minute); err != nil {
		t.Fatalf("GrantLease(a1): %v", err)
	}
	if _, err := s.GrantLease(ctx, "lease-a2", orch.Id, laneA2.Id, laneA2.Revision, "worker-2", time.Minute); !errors.Is(err, ErrProjectAtConcurrencyLimit) {
		t.Fatalf("expected ErrProjectAtConcurrencyLimit for a second mutating lane in project A, got %v", err)
	}
	// A different project must not be affected by project A's limit.
	if _, err := s.GrantLease(ctx, "lease-b1", orch.Id, laneB1.Id, laneB1.Revision, "worker-3", time.Minute); err != nil {
		t.Fatalf("GrantLease(b1) in an independent project: %v", err)
	}
}

// TestHeartbeatRejectsStaleLeaseToken checks that presenting a lease
// token that does not match the lane's current lease fails, rather than
// silently renewing someone else's lease.
func TestHeartbeatRejectsStaleLeaseToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "hb-project")
	task := createTestTask(t, s, orch.Id, "task")
	if _, err := s.RouteParentTask(ctx, "route-1", orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	lane, err := s.CreateLane(ctx, "lane-1", NewID(), orch.Id, proj.Id, task.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	if _, err := s.SyncFrontier(ctx, "sync-1", orch.Id); err != nil {
		t.Fatalf("SyncFrontier: %v", err)
	}
	lane, _ = s.GetLane(ctx, orch.Id, lane.Id)
	leased, err := s.GrantLease(ctx, "lease-1", orch.Id, lane.Id, lane.Revision, "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("GrantLease: %v", err)
	}

	if _, err := s.Heartbeat(ctx, "hb-1", orch.Id, lane.Id, "wrong-token", leased.Revision, time.Minute); !errors.Is(err, ErrLeaseTokenMismatch) {
		t.Fatalf("expected ErrLeaseTokenMismatch, got %v", err)
	}
	running, err := s.Heartbeat(ctx, "hb-2", orch.Id, lane.Id, *leased.LeaseToken, leased.Revision, time.Minute)
	if err != nil {
		t.Fatalf("Heartbeat with correct token: %v", err)
	}
	if running.Status != protocol.DeliveryLaneStatusRunning {
		t.Fatalf("expected running status after heartbeat, got %s", running.Status)
	}
}

// TestExpireLeasesReturnsCrashedWorkAndDoesNotDuplicateAcceptedOutput
// checks that an expired lease returns its lane to runnable so another
// worker can retry, and that ExpireLeases never touches a lane already
// past review.
func TestExpireLeasesReturnsCrashedWorkAndDoesNotDuplicateAcceptedOutput(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	projCrashed := registerProject(t, s, "expire-project-crashed")
	projFinished := registerProject(t, s, "expire-project-finished")

	crashed := createTestTask(t, s, orch.Id, "crashed")
	finished := createTestTask(t, s, orch.Id, "finished")
	if _, err := s.RouteParentTask(ctx, "route-"+crashed.Id, orch.Id, crashed.Id, projCrashed.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	if _, err := s.RouteParentTask(ctx, "route-"+finished.Id, orch.Id, finished.Id, projFinished.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	crashedLane, err := s.CreateLane(ctx, "lane-crashed", NewID(), orch.Id, projCrashed.Id, crashed.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	finishedLane, err := s.CreateLane(ctx, "lane-finished", NewID(), orch.Id, projFinished.Id, finished.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	if _, err := s.SyncFrontier(ctx, "sync-1", orch.Id); err != nil {
		t.Fatalf("SyncFrontier: %v", err)
	}
	crashedLane, _ = s.GetLane(ctx, orch.Id, crashedLane.Id)
	finishedLane, _ = s.GetLane(ctx, orch.Id, finishedLane.Id)

	// crashedLane's worker leases and never heartbeats again (crash).
	leased, err := s.GrantLease(ctx, "lease-crashed", orch.Id, crashedLane.Id, crashedLane.Revision, "worker-doomed", time.Millisecond)
	if err != nil {
		t.Fatalf("GrantLease(crashed): %v", err)
	}
	// finishedLane's worker completes and gets accepted normally.
	leasedFinished, err := s.GrantLease(ctx, "lease-finished", orch.Id, finishedLane.Id, finishedLane.Revision, "worker-ok", time.Minute)
	if err != nil {
		t.Fatalf("GrantLease(finished): %v", err)
	}
	completedFinished, err := s.CompleteLease(ctx, "complete-finished", orch.Id, finishedLane.Id, *leasedFinished.LeaseToken, leasedFinished.Revision)
	if err != nil {
		t.Fatalf("CompleteLease(finished): %v", err)
	}
	acceptedFinished, err := s.UpdateLaneStatus(ctx, "accept-finished", orch.Id, finishedLane.Id, completedFinished.Revision, protocol.DeliveryLaneStatusAccepted)
	if err != nil {
		t.Fatalf("UpdateLaneStatus(accepted): %v", err)
	}

	time.Sleep(5 * time.Millisecond) // let crashedLane's 1ms lease actually pass

	expired, err := s.ExpireLeases(ctx, "expire-1", orch.Id)
	if err != nil {
		t.Fatalf("ExpireLeases: %v", err)
	}
	if len(expired) != 1 || expired[0] != crashedLane.Id {
		t.Fatalf("expected only crashedLane reclaimed, got %v", expired)
	}

	reclaimed, err := s.GetLane(ctx, orch.Id, crashedLane.Id)
	if err != nil {
		t.Fatalf("GetLane(crashed): %v", err)
	}
	if reclaimed.Status != protocol.DeliveryLaneStatusRunnable {
		t.Fatalf("expected crashed lane back to runnable, got %s", reclaimed.Status)
	}
	if reclaimed.LeaseToken != nil {
		t.Fatal("expected lease token cleared on expiry")
	}

	// The already-accepted lane must be completely unaffected.
	stillAccepted, err := s.GetLane(ctx, orch.Id, finishedLane.Id)
	if err != nil {
		t.Fatalf("GetLane(finished): %v", err)
	}
	if stillAccepted.Status != protocol.DeliveryLaneStatusAccepted || stillAccepted.Revision != acceptedFinished.Revision {
		t.Fatalf("expected finished lane untouched by ExpireLeases, got %+v", stillAccepted)
	}

	// The crashed lane's old lease token must never work again - a
	// late-arriving heartbeat from the crashed worker cannot resurrect
	// its old lease, since the lane isn't leased anymore at all.
	if _, err := s.Heartbeat(ctx, "late-hb", orch.Id, crashedLane.Id, *leased.LeaseToken, reclaimed.Revision, time.Minute); !errors.Is(err, ErrLaneNotRunnable) {
		t.Fatalf("expected the crashed worker's late heartbeat to be rejected, got %v", err)
	}
}

func lanesByID(lanes []*protocol.DeliveryLane) map[string]*protocol.DeliveryLane {
	out := make(map[string]*protocol.DeliveryLane, len(lanes))
	for _, l := range lanes {
		out[l.Id] = l
	}
	return out
}
