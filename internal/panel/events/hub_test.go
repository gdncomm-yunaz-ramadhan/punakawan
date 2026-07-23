package events

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestPublishDeliversToSubscriber(t *testing.T) {
	hub := NewHub()
	ch, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSystemReady, OccurredAt: time.Now().UTC()})

	select {
	case evt := <-ch:
		if evt.Type != protocol.PanelEventTypeSystemReady {
			t.Fatalf("Type = %q, want system.ready", evt.Type)
		}
		if evt.Id == "" {
			t.Fatal("expected Publish to assign an id")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the published event")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	hub := NewHub()
	ch, unsubscribe := hub.Subscribe()
	unsubscribe()

	hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSystemReady, OccurredAt: time.Now().UTC()})

	select {
	case evt, ok := <-ch:
		if ok {
			t.Fatalf("received %+v after unsubscribing, want no delivery", evt)
		}
	case <-time.After(50 * time.Millisecond):
		// no delivery, as expected
	}
}

func TestSinceReplaysEventsAfterLastEventID(t *testing.T) {
	hub := NewHub()
	e1 := hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSystemReady, OccurredAt: time.Now().UTC()})
	e2 := hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSessionStarted, OccurredAt: time.Now().UTC()})
	e3 := hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSessionCompleted, OccurredAt: time.Now().UTC()})

	replay := hub.Since(e1.Id)
	if len(replay) != 2 || replay[0].Id != e2.Id || replay[1].Id != e3.Id {
		t.Fatalf("Since(%q) = %+v, want [e2, e3]", e1.Id, replay)
	}
}

func TestSinceUnknownIDReturnsNothing(t *testing.T) {
	hub := NewHub()
	hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSystemReady, OccurredAt: time.Now().UTC()})

	if replay := hub.Since("no-such-id"); replay != nil {
		t.Fatalf("Since(unknown) = %+v, want nil", replay)
	}
}

func TestSinceEmptyReturnsNothing(t *testing.T) {
	hub := NewHub()
	hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSystemReady, OccurredAt: time.Now().UTC()})

	if replay := hub.Since(""); replay != nil {
		t.Fatalf("Since(\"\") = %+v, want nil", replay)
	}
}

func TestSlowSubscriberDoesNotBlockOthers(t *testing.T) {
	hub := NewHub()
	slow, unsubSlow := hub.Subscribe()
	defer unsubSlow()
	fast, unsubFast := hub.Subscribe()
	defer unsubFast()

	// Fill the slow subscriber's buffer without draining it.
	for i := 0; i < clientBuffer+5; i++ {
		hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSystemReady, OccurredAt: time.Now().UTC()})
	}

	select {
	case <-fast:
		// the fast subscriber still received events despite slow's full buffer
	case <-time.After(time.Second):
		t.Fatal("fast subscriber did not receive any event")
	}
	_ = slow
}
