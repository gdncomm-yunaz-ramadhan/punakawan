package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func strptr(s string) *string { return &s }

// TestStartDeliveryRecordsSuppliedDescription covers setting the second
// creation-time descriptive field: unlike a title, a description is
// never derived, so it must arrive exactly as written or not at all.
func TestStartDeliveryRecordsSuppliedDescription(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	view, err := s.StartDeliveryWithOptions(ctx, "", []string{"PAY-1842"}, OrchestrationOptions{
		Title:       "migrate checkout",
		Description: "  Checkout still calls the retired v1 capture endpoint.  ",
	})
	if err != nil {
		t.Fatalf("StartDeliveryWithOptions: %v", err)
	}
	want := "Checkout still calls the retired v1 capture endpoint."
	if view.Description != want {
		t.Fatalf("view.Description = %q, want %q", view.Description, want)
	}
	if view.Orchestration.Description == nil || *view.Orchestration.Description != want {
		t.Fatalf("Orchestration.Description = %v, want the supplied prose persisted", view.Orchestration.Description)
	}

	// A delivery started without one carries no description at all,
	// rather than a placeholder somebody would have to learn to ignore.
	bare, err := s.StartDelivery(ctx, "", []string{"PAY-1843"})
	if err != nil {
		t.Fatalf("StartDelivery: %v", err)
	}
	if bare.Orchestration.Description != nil {
		t.Fatalf("Orchestration.Description = %v, want nil when none was supplied", bare.Orchestration.Description)
	}
	if bare.Description != "" {
		t.Fatalf("view.Description = %q, want empty - nothing derives prose", bare.Description)
	}
	if bare.Title == "" {
		t.Fatal("view.Title is empty; a titleless delivery must still derive a label")
	}
}

// TestUpdateOrchestrationDetailsChangesEachFieldIndependently covers
// updating every mutable field after creation, and the rule that an
// update touches only the fields it names.
func TestUpdateOrchestrationDetailsChangesEachFieldIndependently(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	view, err := s.StartDeliveryWithOptions(ctx, "", []string{"PAY-1842"}, OrchestrationOptions{Title: "first title"})
	if err != nil {
		t.Fatalf("StartDeliveryWithOptions: %v", err)
	}
	id := view.Orchestration.Id

	orch, err := s.UpdateOrchestrationDetails(ctx, NewID(), id, view.Orchestration.Revision, OrchestrationDetails{
		Title:        strptr("second title"),
		Description:  strptr("why this delivery exists"),
		PlanRecordID: strptr("pkw:plan/ws/run-1"),
		SessionID:    strptr("pkw:run/ws/adhoc-17"),
	})
	if err != nil {
		t.Fatalf("UpdateOrchestrationDetails: %v", err)
	}
	if orch.Title == nil || *orch.Title != "second title" {
		t.Fatalf("Title = %v, want the updated title", orch.Title)
	}
	if orch.Description == nil || *orch.Description != "why this delivery exists" {
		t.Fatalf("Description = %v, want the updated description", orch.Description)
	}
	if orch.PlanRecordId == nil || *orch.PlanRecordId != "pkw:plan/ws/run-1" {
		t.Fatalf("PlanRecordId = %v, want the recorded plan reference", orch.PlanRecordId)
	}
	if orch.SessionId == nil || *orch.SessionId != "pkw:run/ws/adhoc-17" {
		t.Fatalf("SessionId = %v, want the recorded session", orch.SessionId)
	}

	// Changing only the title leaves the other three exactly as they
	// were - an edit is not a wholesale replacement of the record.
	orch, err = s.UpdateOrchestrationDetails(ctx, NewID(), id, orch.Revision, OrchestrationDetails{
		Title: strptr("third title"),
	})
	if err != nil {
		t.Fatalf("UpdateOrchestrationDetails (title only): %v", err)
	}
	if orch.Title == nil || *orch.Title != "third title" {
		t.Fatalf("Title = %v, want the third title", orch.Title)
	}
	if orch.Description == nil || *orch.Description != "why this delivery exists" {
		t.Fatalf("Description = %v, want it untouched by a title-only edit", orch.Description)
	}
	if orch.PlanRecordId == nil || orch.SessionId == nil {
		t.Fatalf("plan/session = %v/%v, want both untouched by a title-only edit", orch.PlanRecordId, orch.SessionId)
	}

	// An empty string is how a caller takes a field back. Clearing the
	// title returns the delivery to a derived label rather than leaving
	// it blank.
	orch, err = s.UpdateOrchestrationDetails(ctx, NewID(), id, orch.Revision, OrchestrationDetails{
		Title:       strptr(""),
		Description: strptr("   "),
	})
	if err != nil {
		t.Fatalf("UpdateOrchestrationDetails (clear): %v", err)
	}
	if orch.Title != nil {
		t.Fatalf("Title = %v, want nil after being cleared", orch.Title)
	}
	if orch.Description != nil {
		t.Fatalf("Description = %v, want nil after being cleared", orch.Description)
	}

	cleared, err := s.BuildDeliveryView(ctx, id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if cleared.Title != "PAY-1842" {
		t.Fatalf("view.Title = %q, want the derived label once the stored title is cleared", cleared.Title)
	}
	if cleared.Description != "" {
		t.Fatalf("view.Description = %q, want empty once cleared", cleared.Description)
	}
	if cleared.PlanRecordID != "pkw:plan/ws/run-1" || cleared.SessionID != "pkw:run/ws/adhoc-17" {
		t.Fatalf("view plan/session = %q/%q, want both still surfaced flat on the view", cleared.PlanRecordID, cleared.SessionID)
	}

	if _, err := s.UpdateOrchestrationDetails(ctx, NewID(), id, orch.Revision, OrchestrationDetails{}); err == nil {
		t.Fatal("UpdateOrchestrationDetails with nothing to change succeeded; want an error rather than a revision bump for no reason")
	}
}

// TestUpdateOrchestrationDetailsSetsPlanIDAndRevision covers §4.4's
// replacement pointer: a delivery can carry an exact plan_id+plan_revision
// alongside (not instead of) the deprecated plan_record_id, and either
// can be edited without disturbing the other.
func TestUpdateOrchestrationDetailsSetsPlanIDAndRevision(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	view, err := s.StartDelivery(ctx, "", []string{"PAY-1842"})
	if err != nil {
		t.Fatalf("StartDelivery: %v", err)
	}
	id := view.Orchestration.Id
	revision := 2

	orch, err := s.UpdateOrchestrationDetails(ctx, NewID(), id, view.Orchestration.Revision, OrchestrationDetails{
		PlanID:       strptr("plan-123"),
		PlanRevision: &revision,
	})
	if err != nil {
		t.Fatalf("UpdateOrchestrationDetails: %v", err)
	}
	if orch.PlanId == nil || *orch.PlanId != "plan-123" {
		t.Fatalf("PlanId = %v, want %q", orch.PlanId, "plan-123")
	}
	if orch.PlanRevision == nil || *orch.PlanRevision != 2 {
		t.Fatalf("PlanRevision = %v, want 2", orch.PlanRevision)
	}
	if orch.PlanRecordId != nil {
		t.Fatalf("PlanRecordId = %v, want nil - setting plan_id must not touch the deprecated field", orch.PlanRecordId)
	}

	built, err := s.BuildDeliveryView(ctx, id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if built.PlanID != "plan-123" || built.PlanRevision != 2 {
		t.Fatalf("view plan_id/plan_revision = %q/%d, want %q/%d", built.PlanID, built.PlanRevision, "plan-123", 2)
	}
}

// TestUpdateOrchestrationDetailsStaleRevisionConflicts covers the
// optimistic-locking path: an edit composed against a view somebody else
// has already moved past must conflict, not overwrite.
func TestUpdateOrchestrationDetailsStaleRevisionConflicts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	view, err := s.StartDelivery(ctx, "", []string{"PAY-1842"})
	if err != nil {
		t.Fatalf("StartDelivery: %v", err)
	}
	id := view.Orchestration.Id
	stale := view.Orchestration.Revision

	if _, err := s.UpdateOrchestrationDetails(ctx, NewID(), id, stale, OrchestrationDetails{Title: strptr("landed first")}); err != nil {
		t.Fatalf("first UpdateOrchestrationDetails: %v", err)
	}
	_, err = s.UpdateOrchestrationDetails(ctx, NewID(), id, stale, OrchestrationDetails{Title: strptr("landed second")})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("second update error = %v, want ErrRevisionConflict", err)
	}

	orch, err := s.GetOrchestration(ctx, id)
	if err != nil {
		t.Fatalf("GetOrchestration: %v", err)
	}
	if orch.Title == nil || *orch.Title != "landed first" {
		t.Fatalf("Title = %v, want the first edit to have survived the conflicting second one", orch.Title)
	}
}

// TestAttachAndDetachProject covers project membership end to end: an
// attached project shows up in the view before it has any lane, and
// detaching it removes it again without touching anything else.
func TestAttachAndDetachProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	view, err := s.StartDelivery(ctx, "", []string{"PAY-1842"})
	if err != nil {
		t.Fatalf("StartDelivery: %v", err)
	}
	id := view.Orchestration.Id
	project := registerProject(t, s, "attach-detach")

	orch, err := s.AttachProject(ctx, NewID(), id, view.Orchestration.Revision, project.Id)
	if err != nil {
		t.Fatalf("AttachProject: %v", err)
	}
	if len(orch.ProjectIds) != 1 || orch.ProjectIds[0] != project.Id {
		t.Fatalf("ProjectIds = %v, want exactly the attached project", orch.ProjectIds)
	}

	attached, err := s.BuildDeliveryView(ctx, id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if len(attached.Projects) != 1 {
		t.Fatalf("Projects = %+v, want the attached project even though it has no lanes", attached.Projects)
	}
	summary := attached.Projects[0]
	if summary.ProjectID != project.Id || !summary.Attached {
		t.Fatalf("Projects[0] = %+v, want the attached project marked attached", summary)
	}
	if summary.LaneIDs == nil || len(summary.LaneIDs) != 0 {
		t.Fatalf("LaneIDs = %v, want an empty, non-nil list for a project with no lanes", summary.LaneIDs)
	}
	if summary.CountsByStatus == nil {
		t.Fatal("CountsByStatus is nil; want an empty map so a consumer never has to nil-check it")
	}

	// Attaching the same project again is a no-op rather than a second
	// entry or an error, so a retried call is harmless.
	orch, err = s.AttachProject(ctx, NewID(), id, orch.Revision, project.Id)
	if err != nil {
		t.Fatalf("AttachProject (repeat): %v", err)
	}
	if len(orch.ProjectIds) != 1 {
		t.Fatalf("ProjectIds = %v, want the repeat attach to change nothing", orch.ProjectIds)
	}

	orch, err = s.DetachProject(ctx, NewID(), id, orch.Revision, project.Id)
	if err != nil {
		t.Fatalf("DetachProject: %v", err)
	}
	if len(orch.ProjectIds) != 0 {
		t.Fatalf("ProjectIds = %v, want none after detaching", orch.ProjectIds)
	}

	// Detaching a project the run does not list is not silently accepted.
	if _, err := s.DetachProject(ctx, NewID(), id, orch.Revision, project.Id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DetachProject (already detached) error = %v, want ErrNotFound", err)
	}
	// Attaching a project that was never registered is refused rather
	// than recorded as a dangling id.
	if _, err := s.AttachProject(ctx, NewID(), id, orch.Revision, NewID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AttachProject (unknown project) error = %v, want ErrNotFound", err)
	}
}

// TestDetachProjectRefusesWhileLanesAreUnfinished is the safety case:
// detaching cannot reassign or delete a lane, so a project still running
// work must not be detachable at all.
func TestDetachProjectRefusesWhileLanesAreUnfinished(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	view, err := s.StartDelivery(ctx, "", []string{"PAY-1842"})
	if err != nil {
		t.Fatalf("StartDelivery: %v", err)
	}
	id := view.Orchestration.Id
	project := registerProject(t, s, "still-running")

	orch, err := s.AttachProject(ctx, NewID(), id, view.Orchestration.Revision, project.Id)
	if err != nil {
		t.Fatalf("AttachProject: %v", err)
	}
	lane, err := s.CreateLane(ctx, NewID(), NewID(), id, project.Id, "")
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}

	_, err = s.DetachProject(ctx, NewID(), id, orch.Revision, project.Id)
	if !errors.Is(err, ErrProjectHasActiveLanes) {
		t.Fatalf("DetachProject error = %v, want ErrProjectHasActiveLanes", err)
	}
	// The refusal left the lane exactly as it was - nothing was
	// reassigned, cancelled, or deleted on the way out.
	stillThere, err := s.GetLane(ctx, id, lane.Id)
	if err != nil {
		t.Fatalf("GetLane after refused detach: %v", err)
	}
	if stillThere.ProjectId != project.Id || stillThere.Status != lane.Status {
		t.Fatalf("lane = %+v, want it untouched by the refused detach", stillThere)
	}

	// Once the lane reaches a terminal status the project can be
	// detached, and the finished lane keeps its project - the run still
	// reports honestly where that work happened, marked no longer
	// attached rather than erased.
	if _, err := s.UpdateLaneStatus(ctx, NewID(), id, lane.Id, stillThere.Revision, protocol.DeliveryLaneStatusAccepted); err != nil {
		t.Fatalf("UpdateLaneStatus: %v", err)
	}
	orch, err = s.GetOrchestration(ctx, id)
	if err != nil {
		t.Fatalf("GetOrchestration: %v", err)
	}
	if _, err := s.DetachProject(ctx, NewID(), id, orch.Revision, project.Id); err != nil {
		t.Fatalf("DetachProject after the lane finished: %v", err)
	}

	after, err := s.BuildDeliveryView(ctx, id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if len(after.Projects) != 1 {
		t.Fatalf("Projects = %+v, want the detached project still listed for its finished lane", after.Projects)
	}
	if after.Projects[0].Attached {
		t.Fatalf("Projects[0] = %+v, want attached false after detaching", after.Projects[0])
	}
	if len(after.Projects[0].LaneIDs) != 1 {
		t.Fatalf("LaneIDs = %v, want the finished lane still reported", after.Projects[0].LaneIDs)
	}
}

// TestCreateLaneRecordsSessionId covers the lane-level session: it is
// fixed at creation, and a lane created without one simply names none.
func TestCreateLaneRecordsSessionId(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	view, err := s.StartDelivery(ctx, "", []string{"PAY-1842"})
	if err != nil {
		t.Fatalf("StartDelivery: %v", err)
	}
	id := view.Orchestration.Id
	project := registerProject(t, s, "lane-session")

	withSession, err := s.CreateLaneWithOptions(ctx, NewID(), NewID(), id, project.Id, "", LaneOptions{SessionID: " pkw:run/ws/adhoc-17 "})
	if err != nil {
		t.Fatalf("CreateLaneWithOptions: %v", err)
	}
	if withSession.SessionId == nil || *withSession.SessionId != "pkw:run/ws/adhoc-17" {
		t.Fatalf("SessionId = %v, want the supplied session, trimmed", withSession.SessionId)
	}

	without, err := s.CreateLane(ctx, NewID(), NewID(), id, project.Id, "")
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	if without.SessionId != nil {
		t.Fatalf("SessionId = %v, want nil for a lane created without one", without.SessionId)
	}

	built, err := s.BuildDeliveryView(ctx, id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	sessions := map[string]string{}
	for _, l := range built.Lanes {
		sessions[l.LaneID] = l.SessionID
	}
	if sessions[withSession.Id] != "pkw:run/ws/adhoc-17" {
		t.Fatalf("lane session on the view = %q, want it surfaced", sessions[withSession.Id])
	}
	if sessions[without.Id] != "" {
		t.Fatalf("lane session on the view = %q, want empty for a lane that named none", sessions[without.Id])
	}
}

// TestReplayEventLogWithoutAnyNewEvents is the backwards-compatibility
// case, and the reason it reduces a hand-built log rather than one this
// package just wrote: an orchestration persisted before any of these
// fields existed carries no details_updated event, no project.attached
// event, and payloads with none of the new keys. It must replay into a
// complete, renderable state with no migration and no nil dereference.
func TestReplayEventLogWithoutAnyNewEvents(t *testing.T) {
	orchestrationID := NewID()
	laneID := NewID()
	at := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)

	// Byte-for-byte the payload shapes this package wrote before any of
	// the descriptive fields existed.
	events := []protocol.DeliveryEvent{
		{
			Id: NewID(), OrchestrationId: orchestrationID, IdempotencyKey: "a",
			Type:     protocol.DeliveryEventTypeOrchestrationCreated,
			Payload:  protocol.DeliveryEventPayload{"unresolved_inputs": []interface{}{map[string]interface{}{"reference": "PAY-1842"}}},
			Sequence: 0, OccurredAt: at,
		},
		{
			Id: NewID(), OrchestrationId: orchestrationID, EntityId: &laneID, IdempotencyKey: "b",
			Type:     protocol.DeliveryEventTypeLaneCreated,
			Payload:  protocol.DeliveryEventPayload{"project_id": "legacy-project", "parent_task_id": ""},
			Sequence: 1, OccurredAt: at.Add(time.Minute),
		},
	}

	orch, err := reduceOrchestration(orchestrationID, events)
	if err != nil {
		t.Fatalf("reduceOrchestration: %v", err)
	}
	if orch.Description != nil || orch.PlanRecordId != nil || orch.SessionId != nil {
		t.Fatalf("orchestration = %+v, want every new field absent rather than defaulted to something invented", orch)
	}
	if len(orch.ProjectIds) != 0 {
		t.Fatalf("ProjectIds = %v, want none - this log never attached a project", orch.ProjectIds)
	}
	if orch.Title != nil {
		t.Fatalf("Title = %v, want nil for a log that never carried one", orch.Title)
	}

	lane, err := reduceLane(orchestrationID, laneID, events)
	if err != nil {
		t.Fatalf("reduceLane: %v", err)
	}
	if lane.SessionId != nil {
		t.Fatalf("lane.SessionId = %v, want nil for a lane created before lanes named a session", lane.SessionId)
	}

	// The read model derived from that same log still renders: a real
	// label, no invented prose, and the lane's project reported as
	// present-but-not-attached rather than dropped.
	if label := orchestrationTitle(orch, nil); label != "PAY-1842" {
		t.Fatalf("orchestrationTitle = %q, want a derived label, never a blank one", label)
	}
}

// TestBuildDeliveryViewForOrchestrationWithoutNewEvents is the same
// backfill case one level up, through the real store: an orchestration
// and lane created the way every already-persisted one was must build a
// complete DeliveryView.
func TestBuildDeliveryViewForOrchestrationWithoutNewEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := NewID()

	if _, err := s.CreateOrchestration(ctx, "create-"+id, id, []protocol.DeliveryOrchestrationUnresolvedInputsElem{
		{Reference: "some ambiguous note"},
	}); err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	project := registerProject(t, s, "legacy")
	lane, err := s.CreateLane(ctx, NewID(), NewID(), id, project.Id, "")
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}

	view, err := s.BuildDeliveryView(ctx, id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if view.Title != "some ambiguous note" {
		t.Fatalf("Title = %q, want a derived label", view.Title)
	}
	if view.Description != "" || view.PlanRecordID != "" || view.SessionID != "" {
		t.Fatalf("view = %+v, want every new field empty rather than invented", view)
	}
	if len(view.Projects) != 1 || view.Projects[0].ProjectID != project.Id {
		t.Fatalf("Projects = %+v, want the lane's project still reported", view.Projects)
	}
	if view.Projects[0].Attached {
		t.Fatalf("Projects[0] = %+v, want attached false - this run never attached anything", view.Projects[0])
	}
	if len(view.Lanes) != 1 || view.Lanes[0].LaneID != lane.Id || view.Lanes[0].SessionID != "" {
		t.Fatalf("Lanes = %+v, want the one lane with no session", view.Lanes)
	}
	if view.NextAction == "" {
		t.Fatal("NextAction is empty; an old orchestration must still say what to do next")
	}
}
