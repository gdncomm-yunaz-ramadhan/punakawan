package delivery

import (
	"context"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// TestBuildDeliveryViewFreshOrchestrationWithUnresolvedInputsPromptsToResolve
// covers the highest-priority NextAction case: a fresh, still-pending
// orchestration with unresolved inputs must point the caller at
// answer_delivery_question and name the pending references, not at any
// later-priority condition.
func TestBuildDeliveryViewFreshOrchestrationWithUnresolvedInputsPromptsToResolve(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := NewID()

	orch, err := s.CreateOrchestration(ctx, "create-"+id, id, []protocol.DeliveryOrchestrationUnresolvedInputsElem{
		{Reference: "some ambiguous note"},
	})
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	if orch.Status != protocol.DeliveryOrchestrationStatusPending {
		t.Fatalf("expected pending status, got %s", orch.Status)
	}

	view, err := s.BuildDeliveryView(ctx, orch.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if len(view.PendingQuestions) != 1 || view.PendingQuestions[0] != "some ambiguous note" {
		t.Fatalf("PendingQuestions = %+v, want the one unresolved reference", view.PendingQuestions)
	}
	if !strings.Contains(view.NextAction, "answer_delivery_question") || !strings.Contains(view.NextAction, "some ambiguous note") {
		t.Fatalf("NextAction = %q, want it to mention answer_delivery_question and the pending reference", view.NextAction)
	}
}

// TestBuildDeliveryViewPendingApprovalManifestPromptsToApprove covers the
// second-priority case: once every input is resolved but a manifest is
// still pending, NextAction must point at approve_project_delivery for
// that manifest's project, ahead of any lane-status-derived message.
func TestBuildDeliveryViewPendingApprovalManifestPromptsToApprove(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "approve-me")
	task := createTestTask(t, s, orch.Id, "task")
	if _, err := s.RouteParentTask(ctx, "route-"+NewID(), orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}

	manifest, err := s.CreateApprovalManifest(ctx, "manifest-"+NewID(), NewID(), orch.Id, proj.Id, []string{task.Id}, ManifestPlan{PlannedBaseRef: "main"})
	if err != nil {
		t.Fatalf("CreateApprovalManifest: %v", err)
	}

	view, err := s.BuildDeliveryView(ctx, orch.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if len(view.PendingApprovals) != 1 || view.PendingApprovals[0].Id != manifest.Id {
		t.Fatalf("PendingApprovals = %+v, want exactly the one pending manifest", view.PendingApprovals)
	}
	if !strings.Contains(view.NextAction, "approve_project_delivery") || !strings.Contains(view.NextAction, proj.Id) {
		t.Fatalf("NextAction = %q, want it to mention approve_project_delivery and project %s", view.NextAction, proj.Id)
	}
}

// TestBuildDeliveryViewAllLanesAcceptedSaysComplete covers the "delivery
// complete" case, and also that a lane with zero lanes total never
// vacuously claims completion (computeNextAction guards len(lanes) > 0).
func TestBuildDeliveryViewAllLanesAcceptedSaysComplete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "complete-me")
	task := createTestTask(t, s, orch.Id, "task")
	if _, err := s.RouteParentTask(ctx, "route-"+NewID(), orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	lane, err := s.CreateLane(ctx, "lane-"+NewID(), NewID(), orch.Id, proj.Id, task.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}

	// Zero lanes accepted yet (the lane is still waiting) - must not
	// report complete.
	viewBefore, err := s.BuildDeliveryView(ctx, orch.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView (before): %v", err)
	}
	if viewBefore.NextAction == "delivery complete" {
		t.Fatalf("NextAction reported complete before any lane was accepted: %+v", viewBefore.Lanes)
	}

	if _, err := s.UpdateLaneStatus(ctx, "accept-"+NewID(), orch.Id, lane.Id, lane.Revision, protocol.DeliveryLaneStatusAccepted); err != nil {
		t.Fatalf("UpdateLaneStatus: %v", err)
	}

	view, err := s.BuildDeliveryView(ctx, orch.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if len(view.Lanes) != 1 || view.Lanes[0].Status != protocol.DeliveryLaneStatusAccepted {
		t.Fatalf("Lanes = %+v, want exactly one accepted lane", view.Lanes)
	}
	if view.NextAction != "delivery complete" {
		t.Fatalf("NextAction = %q, want %q", view.NextAction, "delivery complete")
	}
}

// TestBuildDeliveryViewBlockedLaneIsInformational covers the
// blocked-lane priority: a lane genuinely blocked by an unresolved
// dependency (derived the same way SyncFrontier derives it for
// list_runnable_lanes, not a bare status flip) reports informationally
// rather than as an action the caller needs to take, and takes
// priority over the other, still-runnable lane's "in progress" message.
func TestBuildDeliveryViewBlockedLaneIsInformational(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "blocked-lane")

	blocker := createTestTask(t, s, orch.Id, "blocker")
	dependent := createTestTask(t, s, orch.Id, "dependent")
	if _, err := s.RouteParentTask(ctx, "route-blocker-"+NewID(), orch.Id, blocker.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask(blocker): %v", err)
	}
	if _, err := s.RouteParentTask(ctx, "route-dependent-"+NewID(), orch.Id, dependent.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask(dependent): %v", err)
	}
	if _, err := s.AddDependencyEdge(ctx, "edge-"+NewID(), NewID(), orch.Id, dependent.Id, blocker.Id,
		protocol.DependencyEdgeTypeRequires, protocol.DependencyEdgeOriginUser, 1.0, "test fixture"); err != nil {
		t.Fatalf("AddDependencyEdge: %v", err)
	}
	if _, err := s.CreateLane(ctx, "lane-blocker-"+NewID(), NewID(), orch.Id, proj.Id, blocker.Id); err != nil {
		t.Fatalf("CreateLane(blocker): %v", err)
	}
	if _, err := s.CreateLane(ctx, "lane-dependent-"+NewID(), NewID(), orch.Id, proj.Id, dependent.Id); err != nil {
		t.Fatalf("CreateLane(dependent): %v", err)
	}
	if _, err := s.SyncFrontier(ctx, "sync-"+NewID(), orch.Id); err != nil {
		t.Fatalf("SyncFrontier: %v", err)
	}

	view, err := s.BuildDeliveryView(ctx, orch.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if len(view.Blockers) != 1 {
		t.Fatalf("Blockers = %+v, want exactly one blocked lane", view.Blockers)
	}
	if !strings.Contains(view.NextAction, "blocked lane") || !strings.Contains(view.NextAction, "unblock automatically") {
		t.Fatalf("NextAction = %q, want it to describe the blocked lane as informational", view.NextAction)
	}
}

// TestBuildDeliveryViewFailedLaneNeedsHumanReview covers the
// lowest-priority lane-status case: a lane that reached a terminal
// failed status, with nothing higher-priority pending, calls for human
// review.
func TestBuildDeliveryViewFailedLaneNeedsHumanReview(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "failed-lane")
	task := createTestTask(t, s, orch.Id, "task")
	if _, err := s.RouteParentTask(ctx, "route-"+NewID(), orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	lane, err := s.CreateLane(ctx, "lane-"+NewID(), NewID(), orch.Id, proj.Id, task.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	if _, err := s.UpdateLaneStatus(ctx, "fail-"+NewID(), orch.Id, lane.Id, lane.Revision, protocol.DeliveryLaneStatusFailed); err != nil {
		t.Fatalf("UpdateLaneStatus(failed): %v", err)
	}

	view, err := s.BuildDeliveryView(ctx, orch.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if !strings.Contains(view.NextAction, "failed") || !strings.Contains(view.NextAction, "human review") {
		t.Fatalf("NextAction = %q, want it to mention the failed lane needing human review", view.NextAction)
	}
}

// TestBuildDeliveryViewLeavesNewlyRunnableLaneIDsEmpty covers that plain
// BuildDeliveryView never populates NewlyRunnableLaneIDs - only
// BuildDeliveryViewSince opts into that diff.
func TestBuildDeliveryViewLeavesNewlyRunnableLaneIDsEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "plain-view")
	task := createTestTask(t, s, orch.Id, "task")
	if _, err := s.RouteParentTask(ctx, "route-"+NewID(), orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	if _, err := s.CreateLane(ctx, "lane-"+NewID(), NewID(), orch.Id, proj.Id, task.Id); err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	if _, err := s.SyncFrontier(ctx, "sync-"+NewID(), orch.Id); err != nil {
		t.Fatalf("SyncFrontier: %v", err)
	}

	view, err := s.BuildDeliveryView(ctx, orch.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if len(view.NewlyRunnableLaneIDs) != 0 {
		t.Fatalf("NewlyRunnableLaneIDs = %+v, want empty from plain BuildDeliveryView", view.NewlyRunnableLaneIDs)
	}
	if view.LatestSeq <= 0 {
		t.Fatalf("LatestSeq = %d, want it populated regardless of the diff being requested", view.LatestSeq)
	}
}

// TestBuildDeliveryViewSinceReportsLaneUnblockedAfterCheckpoint covers
// AC7's actual diff: a lane blocked at the sinceSeq checkpoint that later
// unblocks is reported; a lane already runnable at that checkpoint is not
// reported again just because it is still runnable.
func TestBuildDeliveryViewSinceReportsLaneUnblockedAfterCheckpoint(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "since-view")

	blocker := createTestTask(t, s, orch.Id, "blocker")
	dependent := createTestTask(t, s, orch.Id, "dependent")
	if _, err := s.RouteParentTask(ctx, "route-blocker-"+NewID(), orch.Id, blocker.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask(blocker): %v", err)
	}
	if _, err := s.RouteParentTask(ctx, "route-dependent-"+NewID(), orch.Id, dependent.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask(dependent): %v", err)
	}
	if _, err := s.AddDependencyEdge(ctx, "edge-"+NewID(), NewID(), orch.Id, dependent.Id, blocker.Id,
		protocol.DependencyEdgeTypeRequires, protocol.DependencyEdgeOriginUser, 1.0, "test fixture"); err != nil {
		t.Fatalf("AddDependencyEdge: %v", err)
	}
	blockerLane, err := s.CreateLane(ctx, "lane-blocker-"+NewID(), NewID(), orch.Id, proj.Id, blocker.Id)
	if err != nil {
		t.Fatalf("CreateLane(blocker): %v", err)
	}
	dependentLane, err := s.CreateLane(ctx, "lane-dependent-"+NewID(), NewID(), orch.Id, proj.Id, dependent.Id)
	if err != nil {
		t.Fatalf("CreateLane(dependent): %v", err)
	}
	syncedLanes, err := s.SyncFrontier(ctx, "sync-1-"+NewID(), orch.Id)
	if err != nil {
		t.Fatalf("SyncFrontier: %v", err)
	}
	blockerLane = lanesByID(syncedLanes)[blockerLane.Id]

	checkpoint, err := s.BuildDeliveryView(ctx, orch.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView (checkpoint): %v", err)
	}

	if _, err := s.UpdateLaneStatus(ctx, "accept-"+NewID(), orch.Id, blockerLane.Id, blockerLane.Revision, protocol.DeliveryLaneStatusAccepted); err != nil {
		t.Fatalf("UpdateLaneStatus(accepted): %v", err)
	}
	if _, err := s.SyncFrontier(ctx, "sync-2-"+NewID(), orch.Id); err != nil {
		t.Fatalf("SyncFrontier (after acceptance): %v", err)
	}

	view, err := s.BuildDeliveryViewSince(ctx, orch.Id, checkpoint.LatestSeq)
	if err != nil {
		t.Fatalf("BuildDeliveryViewSince: %v", err)
	}
	if len(view.NewlyRunnableLaneIDs) != 1 || view.NewlyRunnableLaneIDs[0] != dependentLane.Id {
		t.Fatalf("NewlyRunnableLaneIDs = %+v, want exactly [%s]", view.NewlyRunnableLaneIDs, dependentLane.Id)
	}

	// Since the very beginning (sinceSeq 0), there is no prior baseline
	// for the dependent lane either, so it counts as newly runnable here
	// too - the blocker lane does not, since it is accepted now, not
	// runnable.
	fromStart, err := s.BuildDeliveryViewSince(ctx, orch.Id, 0)
	if err != nil {
		t.Fatalf("BuildDeliveryViewSince (from start): %v", err)
	}
	if len(fromStart.NewlyRunnableLaneIDs) != 1 || fromStart.NewlyRunnableLaneIDs[0] != dependentLane.Id {
		t.Fatalf("NewlyRunnableLaneIDs (from start) = %+v, want exactly [%s]", fromStart.NewlyRunnableLaneIDs, dependentLane.Id)
	}
}

// TestStartDeliveryClassifiesConfidentAndAmbiguousReferencesSeparately
// covers StartDelivery's own split: a confidently classified reference
// becomes a captured requirement source, and an unclassifiable one
// becomes a pending question, in the same call, without erroring.
func TestStartDeliveryClassifiesConfidentAndAmbiguousReferencesSeparately(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	view, err := s.StartDelivery(ctx, "", []string{
		"PAY-1842",
		"https://example.com/spec",
		"acme/checkout#42",
		"a note nobody can classify",
	})
	if err != nil {
		t.Fatalf("StartDelivery: %v", err)
	}
	if len(view.PendingQuestions) != 1 || view.PendingQuestions[0] != "a note nobody can classify" {
		t.Fatalf("PendingQuestions = %+v, want exactly the one unclassifiable reference", view.PendingQuestions)
	}

	sources, err := allRequirementSources(view.Orchestration.Id, mustLoadEvents(t, s, view.Orchestration.Id))
	if err != nil {
		t.Fatalf("allRequirementSources: %v", err)
	}
	if len(sources) != 3 {
		t.Fatalf("captured requirement sources = %d, want 3 (jira, url, github)", len(sources))
	}
}

// mustLoadEvents is a small test helper so
// TestStartDeliveryClassifiesConfidentAndAmbiguousReferencesSeparately can
// inspect captured requirement sources without a dedicated Store method -
// GetRequirementSource requires already knowing the source id, which this
// test does not have.
func mustLoadEvents(t *testing.T, s *Store, orchestrationID string) []protocol.DeliveryEvent {
	t.Helper()
	events, err := loadEvents(context.Background(), s.db.Reader(), orchestrationID)
	if err != nil {
		t.Fatalf("loadEvents: %v", err)
	}
	return events
}

// TestStartDeliveryRepeatedIdempotencyKeyReusesSameOrchestration covers
// StartDelivery's own idempotency contract: retrying with the same
// idempotency key must resolve to the same orchestration rather than
// minting a second one.
func TestStartDeliveryRepeatedIdempotencyKeyReusesSameOrchestration(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, err := s.StartDelivery(ctx, "retry-key", []string{"PAY-1"})
	if err != nil {
		t.Fatalf("StartDelivery (first): %v", err)
	}
	second, err := s.StartDelivery(ctx, "retry-key", []string{"PAY-1"})
	if err != nil {
		t.Fatalf("StartDelivery (retry): %v", err)
	}
	if first.Orchestration.Id != second.Orchestration.Id {
		t.Fatalf("retry minted a different orchestration: first=%s second=%s", first.Orchestration.Id, second.Orchestration.Id)
	}
}
