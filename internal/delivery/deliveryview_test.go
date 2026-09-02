package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/telemetry"
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

// TestBuildDeliveryViewPendingWithNoLanesPromptsToDecompose covers the
// case right after start_delivery: a pending orchestration with no
// unresolved inputs and no lanes yet must not fall through to "no
// pending action". It must name a tool that exists - for a long time it
// named three that did not, leaving no recovery path at all.
func TestBuildDeliveryViewPendingWithNoLanesPromptsToDecompose(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)

	view, err := s.BuildDeliveryView(ctx, orch.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if !strings.Contains(view.NextAction, "no lanes yet") || !strings.Contains(view.NextAction, "start_delivery") {
		t.Fatalf("NextAction = %q, want it to prompt reconciling projects through start_delivery", view.NextAction)
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

// TestBuildDeliveryViewSurfacesLaneDetailAndEvidence covers AC3/AC5: once
// a lane has a worktree, a lease, a recorded Semar stage, and one piece
// of recorded evidence, BuildDeliveryView's LaneSummary must expose all
// of that - worker, worktree/base git state, exactly the recorded stage
// (and no later stage), attempt count, and the evidence as a link-ready
// reference rather than inlined content.
func TestBuildDeliveryViewSurfacesLaneDetailAndEvidence(t *testing.T) {
	f := newWorktreeFixture(t)
	ctx := context.Background()

	if _, err := f.store.SyncFrontier(ctx, "sync-"+NewID(), f.orchestrationID); err != nil {
		t.Fatalf("SyncFrontier: %v", err)
	}
	lane := f.lane(t)

	created, err := f.store.CreateWorktree(ctx, "create-"+NewID(), f.orchestrationID, f.laneID, lane.Revision)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	leased, err := f.store.GrantLease(ctx, "lease-"+NewID(), f.orchestrationID, f.laneID, created.Revision, "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("GrantLease: %v", err)
	}

	semarRecordID := NewID()
	if _, err := f.store.RecordRoleStage(ctx, "semar-"+NewID(), f.orchestrationID, f.laneID, *leased.LeaseToken, RoleStageSemar, semarRecordID, leased.Revision); err != nil {
		t.Fatalf("RecordRoleStage(Semar): %v", err)
	}

	hash, err := f.store.PutArtifact(ctx, []byte("evidence bytes"), "text/plain")
	if err != nil {
		t.Fatalf("PutArtifact: %v", err)
	}
	artifact, err := f.store.RecordArtifact(ctx, "record-"+NewID(), NewID(), ArtifactRef{
		OrchestrationID: f.orchestrationID,
		ProjectID:       f.projectID,
		LaneID:          f.laneID,
		Kind:            protocol.EvidenceArtifactKindCommand,
		Producer:        "go test",
	}, hash)
	if err != nil {
		t.Fatalf("RecordArtifact: %v", err)
	}

	view, err := f.store.BuildDeliveryView(ctx, f.orchestrationID)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if len(view.Lanes) != 1 {
		t.Fatalf("Lanes = %+v, want exactly one", view.Lanes)
	}
	got := view.Lanes[0]

	if got.Worker != "worker-1" {
		t.Fatalf("Worker = %q, want worker-1", got.Worker)
	}
	if got.WorktreePath == "" {
		t.Fatalf("WorktreePath = %q, want it populated", got.WorktreePath)
	}
	if got.BaseSha == "" {
		t.Fatalf("BaseSha = %q, want it populated", got.BaseSha)
	}
	if got.BaseRemote != "origin" {
		t.Fatalf("BaseRemote = %q, want origin", got.BaseRemote)
	}
	if got.SemarRecordID != semarRecordID {
		t.Fatalf("SemarRecordID = %q, want %q", got.SemarRecordID, semarRecordID)
	}
	if got.GarengRecordID != "" || got.PetrukRecordID != "" || got.BagongRecordID != "" {
		t.Fatalf("later-stage record ids should still be empty, got %+v", got)
	}
	if got.Attempt != 1 {
		t.Fatalf("Attempt = %d, want 1", got.Attempt)
	}
	if got.RepairCycleCount != 0 {
		t.Fatalf("RepairCycleCount = %d, want 0 (never repaired)", got.RepairCycleCount)
	}
	if got.EscalatedAt != nil {
		t.Fatalf("EscalatedAt = %v, want nil (never escalated)", got.EscalatedAt)
	}
	if len(got.Evidence) != 1 {
		t.Fatalf("Evidence = %+v, want exactly the one recorded artifact", got.Evidence)
	}
	ev := got.Evidence[0]
	if ev.ID != artifact.Id || ev.ContentHash != hash || ev.MediaType != "text/plain" || ev.Kind != string(protocol.EvidenceArtifactKindCommand) {
		t.Fatalf("Evidence[0] = %+v, want it to match the recorded artifact", ev)
	}
	if ev.ByteSize != len("evidence bytes") {
		t.Fatalf("Evidence[0].ByteSize = %d, want %d", ev.ByteSize, len("evidence bytes"))
	}
}

// TestBuildDeliveryViewEvidenceLookupFailureDegradesGracefully covers
// buildDeliveryView's own graceful-degradation contract: evidence is
// supplementary, so a lane with no recorded evidence simply gets an
// empty Evidence slice rather than the view failing outright, and every
// other field is still populated normally.
func TestBuildDeliveryViewEvidenceLookupFailureDegradesGracefully(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "no-evidence")
	task := createTestTask(t, s, orch.Id, "task")
	if _, err := s.RouteParentTask(ctx, "route-"+NewID(), orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	if _, err := s.CreateLane(ctx, "lane-"+NewID(), NewID(), orch.Id, proj.Id, task.Id); err != nil {
		t.Fatalf("CreateLane: %v", err)
	}

	view, err := s.BuildDeliveryView(ctx, orch.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if len(view.Lanes) != 1 || view.Lanes[0].Evidence != nil {
		t.Fatalf("Lanes = %+v, want exactly one lane with a nil/empty Evidence slice", view.Lanes)
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

// TestStartDeliverySuppliedTitleWinsOverDerivation covers the plain case:
// a caller who says what the delivery is for gets exactly that back, both
// persisted on the orchestration and surfaced on the view, with no
// derivation applied on top of it.
func TestStartDeliverySuppliedTitleWinsOverDerivation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	view, err := s.StartDeliveryWithOptions(ctx, "", []string{"PAY-1842", "PAY-1843"}, OrchestrationOptions{
		Title: "  migrate checkout to the new payments API  ",
	})
	if err != nil {
		t.Fatalf("StartDeliveryWithOptions: %v", err)
	}
	if view.Title != "migrate checkout to the new payments API" {
		t.Fatalf("Title = %q, want the supplied title, trimmed", view.Title)
	}
	if view.Orchestration.Title == nil || *view.Orchestration.Title != "migrate checkout to the new payments API" {
		t.Fatalf("Orchestration.Title = %v, want the supplied title persisted on the orchestration itself", view.Orchestration.Title)
	}

	// Rebuilding from the event log must produce the same title: a
	// supplied title is state, not something recomputed per call.
	reread, err := s.BuildDeliveryView(ctx, view.Orchestration.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if reread.Title != view.Title {
		t.Fatalf("Title after replay = %q, want %q", reread.Title, view.Title)
	}
}

func TestDeliveryViewIncludesHighLevelAndProjectPlans(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	view, err := s.StartDeliveryWithOptions(ctx, "plan-linked-delivery", []string{"PAY-1842"}, OrchestrationOptions{
		Title: "Migrate billing to v2", Description: "Move every billing caller onto the v2 pricing endpoint.",
		PlanID: "delivery-plan", PlanRevision: 3,
	})
	if err != nil {
		t.Fatalf("StartDeliveryWithOptions: %v", err)
	}
	project := registerProject(t, s, "billing")
	seedPlanRevision(t, s, "billing-plan", 1, []string{project.Id})
	seedPlanRevision(t, s, "billing-plan", 2, []string{project.Id})
	if err := s.LinkProjectPlan(ctx, "link-billing-plan", view.Orchestration.Id, project.Id, "billing-plan", 2); err != nil {
		t.Fatalf("LinkProjectPlan: %v", err)
	}

	got, err := s.BuildDeliveryView(ctx, view.Orchestration.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if got.PlanID != "delivery-plan" || got.PlanRevision != 3 {
		t.Fatalf("high-level plan = %q r%d, want delivery-plan r3", got.PlanID, got.PlanRevision)
	}
	if len(got.ProjectPlans) != 1 {
		t.Fatalf("ProjectPlans = %+v, want one linked project plan", got.ProjectPlans)
	}
	link := got.ProjectPlans[0]
	if link.ProjectID != project.Id || link.PlanID != "billing-plan" || link.PlanRevision != 2 {
		t.Fatalf("ProjectPlans[0] = %+v, want project %s plan billing-plan r2", link, project.Id)
	}
}

func TestDeliveryViewIncludesCapturedJiraSnapshotAsActivity(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	resolved, err := s.StartOrResolveExecution(ctx, "resolve-jira-activity", SourceIdentity{Kind: SourceKindJira, Provider: "jira", Tenant: "test-tenant", Key: "TRF-19272"}, OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution: %v", err)
	}
	view, err := s.BuildDeliveryView(ctx, resolved.Execution.OrchestrationID)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if len(view.JiraActivity) != 1 {
		t.Fatalf("JiraActivity = %+v, want one captured source activity", view.JiraActivity)
	}
	activity := view.JiraActivity[0]
	if activity.EventType != "source.snapshot_captured" || activity.IssueKey != "TRF-19272" {
		t.Fatalf("JiraActivity[0] = %+v, want captured TRF-19272 source", activity)
	}
}

// TestDeliveryViewDerivesTitleFromSingleReference covers the one-reference
// derivation: with no title supplied, the view must name the single
// requirement rather than fall back to a count or an empty string, and
// must not append a "+N more" suffix when there is nothing more.
func TestDeliveryViewDerivesTitleFromSingleReference(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	view, err := s.StartDelivery(ctx, "", []string{"PAY-1842"})
	if err != nil {
		t.Fatalf("StartDelivery: %v", err)
	}
	if view.Orchestration.Title != nil {
		t.Fatalf("Orchestration.Title = %v, want nil - nothing was supplied, so nothing should be persisted", view.Orchestration.Title)
	}
	if view.Title != "PAY-1842" {
		t.Fatalf("Title = %q, want the single reference itself", view.Title)
	}

	// Once that reference is enriched with a real title, the derived
	// label follows it - the derivation reads whatever the captured
	// source now carries, it does not freeze the bare reference string.
	if _, err := s.CaptureRequirement(ctx, NewID(), view.Orchestration.Id, SourceInput{
		Provider: "jira", ExternalID: "PAY-1842", Title: "Split payment capture from authorization",
	}); err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
	enriched, err := s.BuildDeliveryView(ctx, view.Orchestration.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if enriched.Title != "Split payment capture from authorization" {
		t.Fatalf("Title = %q, want the captured source's own title", enriched.Title)
	}
}

// TestDeliveryViewDerivesTitleForMultipleReferences covers the
// multi-reference derivation: the earliest captured requirement names the
// delivery and every other requirement - captured or still pending - is
// counted, so the label stays one line no matter how many references came
// in.
func TestDeliveryViewDerivesTitleForMultipleReferences(t *testing.T) {
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
	// Three captured sources plus one pending question is four
	// requirements, so the first one is named and the other three counted.
	if view.Title != "PAY-1842 (+3 more)" {
		t.Fatalf("Title = %q, want the first reference plus a count of the rest", view.Title)
	}
}

// TestDeliveryViewDerivesTitleWithoutAnyTitleEvent is the backfill case:
// an orchestration whose event log predates titles entirely (its
// orchestration.created payload carries no title key at all) must still
// render a readable label rather than an empty string.
func TestDeliveryViewDerivesTitleWithoutAnyTitleEvent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := NewID()

	// CreateOrchestration writes an orchestration.created payload with no
	// title key, which is byte-for-byte the shape every already-persisted
	// orchestration has.
	orch, err := s.CreateOrchestration(ctx, "create-"+id, id, []protocol.DeliveryOrchestrationUnresolvedInputsElem{
		{Reference: "some ambiguous note"},
	})
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	if orch.Title != nil {
		t.Fatalf("Orchestration.Title = %v, want nil for a log with no title", orch.Title)
	}

	view, err := s.BuildDeliveryView(ctx, orch.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if view.Title != "some ambiguous note" {
		t.Fatalf("Title = %q, want the pending reference - a titleless log must never render a blank label", view.Title)
	}

	// An orchestration with no requirements at all is the last resort and
	// still must not be blank.
	bare := NewID()
	if _, err := s.CreateOrchestration(ctx, "create-"+bare, bare, nil); err != nil {
		t.Fatalf("CreateOrchestration (bare): %v", err)
	}
	bareView, err := s.BuildDeliveryView(ctx, bare)
	if err != nil {
		t.Fatalf("BuildDeliveryView (bare): %v", err)
	}
	if bareView.Title == "" {
		t.Fatal("Title is empty for an orchestration with no requirements; want a non-empty last-resort label")
	}
}

// TestDeliveryViewSurfacesCumulativeTelemetry asserts BuildDeliveryView
// reports the same cumulative usage internal/telemetry itself computes,
// and that it is additive across two sessions on the same orchestration.
func TestDeliveryViewSurfacesCumulativeTelemetry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := NewID()

	if _, err := s.CreateOrchestration(ctx, "create-"+id, id, []protocol.DeliveryOrchestrationUnresolvedInputsElem{{Reference: "note"}}); err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}

	view, err := s.BuildDeliveryView(ctx, id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if view.Telemetry.Counters.InputTokens != 0 {
		t.Fatalf("Telemetry.Counters.InputTokens = %d, want 0 before any session begins", view.Telemetry.Counters.InputTokens)
	}

	tstore := telemetry.NewStore(s.db)
	sessA, err := tstore.Begin(ctx, telemetry.BeginRequest{DeliveryID: id, ClientKind: "claude-code", ExternalSessionID: "sess-a"})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	sessB, err := tstore.Begin(ctx, telemetry.BeginRequest{DeliveryID: id, ClientKind: "codex", ExternalSessionID: "sess-b"})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := tstore.IngestSnapshot(ctx, telemetry.SnapshotRequest{SessionID: sessA.ID, SourceID: "main", Sequence: 1, InputTokens: 10}); err != nil {
		t.Fatalf("IngestSnapshot: %v", err)
	}
	if _, err := tstore.IngestSnapshot(ctx, telemetry.SnapshotRequest{SessionID: sessB.ID, SourceID: "main", Sequence: 1, InputTokens: 30}); err != nil {
		t.Fatalf("IngestSnapshot: %v", err)
	}

	view, err = s.BuildDeliveryView(ctx, id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if view.Telemetry.Counters.InputTokens != 40 {
		t.Fatalf("Telemetry.Counters.InputTokens = %d, want 40 (additive across both sessions)", view.Telemetry.Counters.InputTokens)
	}
}

// TestAllOrchestrationStatesMatchesPerOrchestrationLookup covers the
// batch path a list projection uses instead of calling GetOrchestration
// once per id: for a handful of orchestrations with distinct titles and
// statuses, the batched result must report the exact same orchestration
// record and derived title GetOrchestration/BuildDeliveryView would each
// report individually.
func TestAllOrchestrationStatesMatchesPerOrchestrationLookup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := createTestOrchestration(t, s)
	second, err := s.CreateOrchestrationWithOptions(ctx, "create-second", NewID(), nil, OrchestrationOptions{Title: "Second delivery"})
	if err != nil {
		t.Fatalf("CreateOrchestrationWithOptions: %v", err)
	}
	if _, err := s.CancelOrchestration(ctx, "cancel-second", second.Id, second.Revision); err != nil {
		t.Fatalf("CancelOrchestration: %v", err)
	}

	states, ids, err := s.AllOrchestrationStates(ctx)
	if err != nil {
		t.Fatalf("AllOrchestrationStates: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ids = %+v, want exactly the 2 seeded orchestrations", ids)
	}

	firstView, err := s.BuildDeliveryView(ctx, first.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView(first): %v", err)
	}
	firstState, ok := states[first.Id]
	if !ok {
		t.Fatalf("states missing %s", first.Id)
	}
	if firstState.Title != firstView.Title || firstState.Orchestration.Status != firstView.Orchestration.Status {
		t.Fatalf("first state = %+v, want title %q status %s", firstState, firstView.Title, firstView.Orchestration.Status)
	}

	secondState, ok := states[second.Id]
	if !ok {
		t.Fatalf("states missing %s", second.Id)
	}
	if secondState.Title != "Second delivery" {
		t.Fatalf("second title = %q, want %q", secondState.Title, "Second delivery")
	}
	if secondState.Orchestration.Status != protocol.DeliveryOrchestrationStatusCancelled {
		t.Fatalf("second status = %s, want cancelled", secondState.Orchestration.Status)
	}
}
