package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/deliveryhooks"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// recordingHook is a deliveryhooks.Hook that just remembers every Event it
// is handed, in order, for tests to assert against - a fake rather than a
// mock, since nothing here needs to assert *how* Dispatch called it, only
// what it was called with.
type recordingHook struct {
	events []deliveryhooks.Event
}

func (h *recordingHook) Handle(ctx context.Context, event deliveryhooks.Event) error {
	h.events = append(h.events, event)
	return nil
}

// failingHook always returns an error, for proving that a hook failure
// never propagates out of the Store method that dispatched to it.
type failingHook struct{ called bool }

func (h *failingHook) Handle(ctx context.Context, event deliveryhooks.Event) error {
	h.called = true
	return errors.New("simulated hook failure")
}

func newHookTestStore(t *testing.T, hooks ...deliveryhooks.Hook) *Store {
	t.Helper()
	return NewStore(newTestDB(t), WithHooks(hooks...))
}

func TestCreateOrchestrationDispatchesDeliveryStarted(t *testing.T) {
	rec := &recordingHook{}
	s := newHookTestStore(t, rec)
	ctx := context.Background()
	id := NewID()

	orch, err := s.CreateOrchestrationWithOptions(ctx, "create-"+id, id, nil, OrchestrationOptions{Title: "Refund API delivery"})
	if err != nil {
		t.Fatalf("CreateOrchestrationWithOptions: %v", err)
	}

	if len(rec.events) != 1 {
		t.Fatalf("events = %+v, want exactly one delivery.started dispatch", rec.events)
	}
	ev := rec.events[0]
	if ev.Type != deliveryhooks.EventDeliveryStarted {
		t.Fatalf("event type = %s, want %s", ev.Type, deliveryhooks.EventDeliveryStarted)
	}
	if ev.DeliveryID != id || ev.Revision != orch.Revision || ev.Title != "Refund API delivery" {
		t.Fatalf("event = %+v, want delivery_id=%s revision=%d title=%q", ev, id, orch.Revision, "Refund API delivery")
	}

	// A retried create (same idempotency key) hits ErrDuplicateWrite and
	// must not re-announce "delivery started".
	if _, err := s.CreateOrchestrationWithOptions(ctx, "create-"+id, id, nil, OrchestrationOptions{Title: "Refund API delivery"}); err != nil {
		t.Fatalf("retried CreateOrchestrationWithOptions: %v", err)
	}
	if len(rec.events) != 1 {
		t.Fatalf("events after retry = %+v, want still exactly one dispatch", rec.events)
	}
}

func TestUpdateOrchestrationDetailsDispatchesPlanCreatedThenRevised(t *testing.T) {
	rec := &recordingHook{}
	s := newHookTestStore(t, rec)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	rec.events = nil // discard the delivery.started dispatch from creating the fixture orchestration above

	planID := "plan-123"
	rev1 := 1
	updated, err := s.UpdateOrchestrationDetails(ctx, "update-1", orch.Id, orch.Revision, OrchestrationDetails{PlanID: &planID, PlanRevision: &rev1})
	if err != nil {
		t.Fatalf("UpdateOrchestrationDetails (first plan attach): %v", err)
	}
	if len(rec.events) != 1 || rec.events[0].Type != deliveryhooks.EventPlanCreated {
		t.Fatalf("events = %+v, want exactly one plan.created dispatch", rec.events)
	}
	if rec.events[0].PlanID != planID || rec.events[0].PlanRevision != rev1 {
		t.Fatalf("plan.created event = %+v, want plan_id=%s plan_revision=%d", rec.events[0], planID, rev1)
	}

	rev2 := 2
	if _, err := s.UpdateOrchestrationDetails(ctx, "update-2", orch.Id, updated.Revision, OrchestrationDetails{PlanRevision: &rev2}); err != nil {
		t.Fatalf("UpdateOrchestrationDetails (plan revision bump): %v", err)
	}
	if len(rec.events) != 2 || rec.events[1].Type != deliveryhooks.EventPlanRevised {
		t.Fatalf("events = %+v, want a second dispatch of type plan.revised", rec.events)
	}
	if rec.events[1].PlanRevision != rev2 {
		t.Fatalf("plan.revised event = %+v, want plan_revision=%d", rec.events[1], rev2)
	}

	// A detail update that touches neither PlanID nor PlanRevision must not
	// dispatch a plan event at all.
	title := "renamed"
	if _, err := s.UpdateOrchestrationDetails(ctx, "update-3", orch.Id, rec.events[1].Revision, OrchestrationDetails{Title: &title}); err != nil {
		t.Fatalf("UpdateOrchestrationDetails (title only): %v", err)
	}
	if len(rec.events) != 2 {
		t.Fatalf("events = %+v, want no additional dispatch for a title-only update", rec.events)
	}
}

func TestCancelOrchestrationDispatchesDeliveryFailed(t *testing.T) {
	rec := &recordingHook{}
	s := newHookTestStore(t, rec)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	rec.events = nil // discard the delivery.started dispatch from creating the fixture orchestration above

	cancelled, err := s.CancelOrchestration(ctx, "cancel-1", orch.Id, orch.Revision)
	if err != nil {
		t.Fatalf("CancelOrchestration: %v", err)
	}
	if len(rec.events) != 1 || rec.events[0].Type != deliveryhooks.EventDeliveryFailed {
		t.Fatalf("events = %+v, want exactly one delivery.failed dispatch", rec.events)
	}
	if rec.events[0].Revision != cancelled.Revision {
		t.Fatalf("event revision = %d, want %d", rec.events[0].Revision, cancelled.Revision)
	}
}

func TestGrantAndCompleteLeaseDispatchImplementationEvents(t *testing.T) {
	rec := &recordingHook{}
	s := newHookTestStore(t, rec)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "hooks-project")
	lane, err := s.CreateLane(ctx, "lane-1", NewID(), orch.Id, proj.Id, "")
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	// This lane has no parent task, so SyncFrontier never considers it (it
	// only tracks task-routed lanes) - move it to runnable directly, the
	// same way deliveryview_test.go's fixtures do for a lane outside the
	// graph-driven frontier flow.
	lane, err = s.UpdateLaneStatus(ctx, "runnable-1", orch.Id, lane.Id, lane.Revision, protocol.DeliveryLaneStatusRunnable)
	if err != nil {
		t.Fatalf("UpdateLaneStatus(runnable): %v", err)
	}
	rec.events = nil // discard the delivery.started dispatch from creating the fixture orchestration above

	leased, err := s.GrantLease(ctx, "lease-1", orch.Id, lane.Id, lane.Revision, "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("GrantLease: %v", err)
	}
	if len(rec.events) != 1 || rec.events[0].Type != deliveryhooks.EventImplementationStarted {
		t.Fatalf("events = %+v, want exactly one implementation.started dispatch", rec.events)
	}

	if _, err := s.CompleteLease(ctx, "complete-1", orch.Id, lane.Id, *leased.LeaseToken, leased.Revision); err != nil {
		t.Fatalf("CompleteLease: %v", err)
	}
	if len(rec.events) != 2 || rec.events[1].Type != deliveryhooks.EventImplementationCompleted {
		t.Fatalf("events = %+v, want a second dispatch of type implementation.completed", rec.events)
	}
}

func TestRecordReviewConclusionDispatchesAcceptedOrChangesRequired(t *testing.T) {
	rec := &recordingHook{}
	s := newHookTestStore(t, rec)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "hooks-review-project")
	lane, err := s.CreateLane(ctx, "lane-1", NewID(), orch.Id, proj.Id, "")
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	rec.events = nil // discard the delivery.started dispatch from creating the fixture orchestration above

	approved := baseReviewConclusion()
	approved.ReviewerSessionId = "session-reviewer"
	approved.IndependenceLevel = protocol.ReviewConclusionIndependenceLevelDifferentSession
	if _, err := s.RecordReviewConclusion(ctx, "rc-1", orch.Id, lane.Id, approved, "session-implementer", lane.Revision); err != nil {
		t.Fatalf("RecordReviewConclusion (approved): %v", err)
	}
	if len(rec.events) != 1 || rec.events[0].Type != deliveryhooks.EventReviewAccepted {
		t.Fatalf("events = %+v, want exactly one review.accepted dispatch", rec.events)
	}

	lane, err = s.GetLane(ctx, orch.Id, lane.Id)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	blocked := baseReviewConclusion()
	blocked.Outcome = protocol.ReviewConclusionOutcomeChangesRequested
	blocked.BlockingFindingIds = []string{"finding-1"}
	blocked.ReviewerSessionId = "session-reviewer"
	blocked.IndependenceLevel = protocol.ReviewConclusionIndependenceLevelDifferentSession
	if _, err := s.RecordReviewConclusion(ctx, "rc-2", orch.Id, lane.Id, blocked, "session-implementer", lane.Revision); err != nil {
		t.Fatalf("RecordReviewConclusion (changes requested): %v", err)
	}
	if len(rec.events) != 2 || rec.events[1].Type != deliveryhooks.EventReviewChangesRequired {
		t.Fatalf("events = %+v, want a second dispatch of type review.changes_required", rec.events)
	}
}

func TestHookFailureDoesNotFailTheUnderlyingDeliveryOperation(t *testing.T) {
	fh := &failingHook{}
	s := newHookTestStore(t, fh)
	ctx := context.Background()
	id := NewID()

	if _, err := s.CreateOrchestration(ctx, "create-"+id, id, nil); err != nil {
		t.Fatalf("CreateOrchestration must succeed even though its hook fails: %v", err)
	}
	if !fh.called {
		t.Fatal("expected the failing hook to have been invoked")
	}
}
