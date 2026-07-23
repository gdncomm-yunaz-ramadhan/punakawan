package events

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestSSEHandlerSendsSystemReadyOnFreshConnect(t *testing.T) {
	hub := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("GET", "/api/v1/events", nil).WithContext(ctx)

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		SSEHandler(hub)(rec, req)
		close(done)
	}()

	// Give the handler time to write its first frame, then cancel so it
	// returns instead of blocking on the live-event loop forever.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if !strings.Contains(rec.Body.String(), "system.ready") {
		t.Fatalf("body = %q, want it to contain system.ready", rec.Body.String())
	}
}

func TestSSEHandlerReplaysSinceLastEventID(t *testing.T) {
	hub := NewHub()
	e1 := hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSystemReady, OccurredAt: time.Now().UTC()})
	hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSessionStarted, OccurredAt: time.Now().UTC()})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("GET", "/api/v1/events", nil).WithContext(ctx)
	req.Header.Set("Last-Event-ID", e1.Id)

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		SSEHandler(hub)(rec, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()
	if strings.Contains(body, "system.ready") {
		t.Fatalf("body = %q, want no system.ready on a resumed connection", body)
	}
	if !strings.Contains(body, "session.started") {
		t.Fatalf("body = %q, want the replayed session.started event", body)
	}
}
