// Package deliveryhooks defines the higher-level delivery-event vocabulary
// external integrations (a linked Jira issue, eventually others) react to,
// and the Hook interface they implement to do so.
//
// internal/delivery already appends a granular, exhaustive event log for
// its own state replay (orchestration.created, lane.status_changed, and so
// on - see internal/delivery/events.go and reduce.go). That log is not
// reused directly here: it exists to let a Store rebuild exact state from
// scratch, so every entry it carries matters to that replay and none of it
// can be renamed or removed without breaking it. This package instead
// defines a small, deliberately coarser set of named moments - "a delivery
// started", "a review came back needing changes" - that an external
// integration can subscribe to without knowing anything about how
// internal/delivery represents its own state internally. internal/delivery
// translates specific state transitions into these events and hands them to
// whatever Hook a caller has registered; it never depends on any concrete
// Hook implementation itself, only on this package's Event type and Hook
// interface, so adding or changing an integration never touches
// internal/delivery's own code.
package deliveryhooks

import (
	"context"
	"log/slog"
)

// EventType names one of the higher-level delivery moments a Hook can react
// to.
type EventType string

const (
	// EventDeliveryStarted fires once, when a delivery orchestration is
	// first created.
	EventDeliveryStarted EventType = "delivery.started"
	// EventPlanCreated fires the first time a delivery's plan_id/plan_revision
	// are set.
	EventPlanCreated EventType = "plan.created"
	// EventPlanRevised fires on every later change to a delivery's
	// plan_id/plan_revision, after EventPlanCreated has already fired once.
	EventPlanRevised EventType = "plan.revised"
	// EventImplementationStarted fires when a lane's lease is granted -
	// implementation work on that lane begins.
	EventImplementationStarted EventType = "implementation.started"
	// EventImplementationCompleted fires when a lane's lease is reported
	// complete - the leaseholder's work is done and it has moved to review.
	EventImplementationCompleted EventType = "implementation.completed"
	// EventReviewChangesRequired fires when a Bagong review conclusion is
	// recorded with an outcome other than approved (i.e. it found something
	// blocking).
	EventReviewChangesRequired EventType = "review.changes_required"
	// EventReviewAccepted fires when a Bagong review conclusion is recorded
	// with an approved outcome.
	EventReviewAccepted EventType = "review.accepted"
	// EventDeliveryCompleted fires when a delivery orchestration reaches its
	// completed terminal status.
	EventDeliveryCompleted EventType = "delivery.completed"
	// EventDeliveryFailed fires when a delivery orchestration reaches a
	// terminal status other than completed (e.g. cancelled).
	EventDeliveryFailed EventType = "delivery.failed"
)

// Event is the payload handed to a Hook for one fired EventType. DeliveryID
// is always the delivery orchestration id; Revision is that orchestration's
// derived revision at the moment this event was raised, which together with
// DeliveryID and Type forms the deterministic idempotency key
// ("<delivery-id>:<event-type>:<revision>") a Hook implementation should use
// to make a retried dispatch a safe no-op instead of a duplicate external
// side effect.
type Event struct {
	Type         EventType
	DeliveryID   string
	Revision     int
	Title        string
	Projects     []string
	PullRequests []string
	PlanID       string
	PlanRevision int
	// Summary is a short, human-readable description of what happened,
	// good enough for a Hook to use verbatim in a notification if it has
	// nothing more specific to say.
	Summary string
}

// Hook reacts to one fired Event. Handle is expected to be safe to call
// repeatedly with an Event carrying the same DeliveryID/Type/Revision (see
// Event's own doc comment on the idempotency key those three fields form) -
// a Hook that cannot guarantee that must do its own internal deduplication
// rather than rely on only ever being called once.
type Hook interface {
	Handle(ctx context.Context, event Event) error
}

// Dispatcher fans one Event out to every registered Hook, catching and
// logging (never propagating) whatever error each Hook returns. A hook
// reacting to a delivery event is inherently a side effect on top of the
// delivery operation that raised it (e.g. posting a Jira comment after a
// review conclusion is recorded) and must never be able to fail or roll
// back that underlying operation just because the side effect could not be
// applied - the delivery state change has already committed by the time
// Dispatch runs.
type Dispatcher struct {
	hooks []Hook
}

// NewDispatcher builds a Dispatcher over hooks. Called with no hooks, the
// returned Dispatcher's Dispatch is a complete no-op, same as a nil
// *Dispatcher - so a caller that has nothing configured to hook into never
// has to special-case that.
func NewDispatcher(hooks ...Hook) *Dispatcher {
	return &Dispatcher{hooks: hooks}
}

// Dispatch calls Handle on every registered hook with event. d may be nil
// (a Store built without any hooks configured), in which case Dispatch does
// nothing - this is what makes wiring hooks into a Store entirely additive
// and opt-in: every existing caller that never configures a Dispatcher gets
// the exact same no-op behavior it always had.
func (d *Dispatcher) Dispatch(ctx context.Context, event Event) {
	if d == nil {
		return
	}
	for _, h := range d.hooks {
		if err := h.Handle(ctx, event); err != nil {
			slog.Warn("deliveryhooks: hook failed, delivery operation is unaffected",
				"event_type", event.Type, "delivery_id", event.DeliveryID, "revision", event.Revision, "error", err)
		}
	}
}
