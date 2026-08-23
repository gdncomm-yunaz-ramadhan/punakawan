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
		SSEHandler(hub, context.Background())(rec, req)
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
		SSEHandler(hub, context.Background())(rec, req)
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

// TestSSEHandlerReturnsOnShutdownSignalWithoutClientDisconnect proves the
// regression this handler exists to prevent: a connection whose *client*
// never disconnects (r.Context() stays live for the whole test) must still
// return promptly once the server's own shutdown signal fires. Before
// shutdownCtx existed, the handler's only exit paths were r.Context().Done()
// and the hub channel closing, so this same scenario would hang until the
// test's own timeout - exactly the bug that made a real panel shutdown block
// on any open browser tab.
func TestSSEHandlerReturnsOnShutdownSignalWithoutClientDisconnect(t *testing.T) {
	hub := NewHub()
	// The request's own context is deliberately never cancelled here: this
	// test is only valid if the handler exits via shutdownCtx, not by
	// piggybacking on a client disconnect.
	req := httptest.NewRequest("GET", "/api/v1/events", nil)

	shutdownCtx, cancelShutdown := context.WithCancel(context.Background())
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		SSEHandler(hub, shutdownCtx)(rec, req)
		close(done)
	}()

	// Let the handler past its first-frame write and into the live-event
	// select loop before firing shutdown.
	time.Sleep(50 * time.Millisecond)
	cancelShutdown()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSEHandler did not return after the shutdown context was cancelled")
	}
}
