package deliveryhooks

import (
	"context"
	"errors"
	"testing"
)

type recordingHook struct {
	handled []Event
}

func (h *recordingHook) Handle(ctx context.Context, event Event) error {
	h.handled = append(h.handled, event)
	return nil
}

type erroringHook struct{ calls int }

func (h *erroringHook) Handle(ctx context.Context, event Event) error {
	h.calls++
	return errors.New("simulated failure")
}

func TestNilDispatcherDispatchIsANoOp(t *testing.T) {
	var d *Dispatcher
	// Must not panic even though the receiver is nil - this is what makes
	// a Store with no hooks configured pay nothing for dispatch calls.
	d.Dispatch(context.Background(), Event{Type: EventDeliveryStarted, DeliveryID: "d1"})
}

func TestDispatcherWithNoHooksIsANoOp(t *testing.T) {
	d := NewDispatcher()
	d.Dispatch(context.Background(), Event{Type: EventDeliveryStarted, DeliveryID: "d1"})
}

func TestDispatchCallsEveryRegisteredHook(t *testing.T) {
	h1 := &recordingHook{}
	h2 := &recordingHook{}
	d := NewDispatcher(h1, h2)

	ev := Event{Type: EventDeliveryCompleted, DeliveryID: "d1", Revision: 3}
	d.Dispatch(context.Background(), ev)

	if len(h1.handled) != 1 || h1.handled[0].Type != ev.Type || h1.handled[0].DeliveryID != ev.DeliveryID || h1.handled[0].Revision != ev.Revision {
		t.Fatalf("h1.handled = %+v, want exactly one copy of %+v", h1.handled, ev)
	}
	if len(h2.handled) != 1 || h2.handled[0].Type != ev.Type || h2.handled[0].DeliveryID != ev.DeliveryID || h2.handled[0].Revision != ev.Revision {
		t.Fatalf("h2.handled = %+v, want exactly one copy of %+v", h2.handled, ev)
	}
}

func TestDispatchSwallowsHookErrorsAndKeepsGoing(t *testing.T) {
	failing := &erroringHook{}
	after := &recordingHook{}
	d := NewDispatcher(failing, after)

	// Must not panic and must still reach the hook registered after the
	// failing one - one hook's error must never block another's delivery.
	d.Dispatch(context.Background(), Event{Type: EventDeliveryFailed, DeliveryID: "d1"})

	if failing.calls != 1 {
		t.Fatalf("failing.calls = %d, want 1", failing.calls)
	}
	if len(after.handled) != 1 {
		t.Fatalf("after.handled = %+v, want exactly one event despite the earlier hook failing", after.handled)
	}
}
