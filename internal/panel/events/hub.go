// Package events implements the Punakawan Panel's live-update pipeline:
// an in-process pub/sub Hub, an SSE handler over it, and a Reconciler
// that polls the existing readers and publishes PanelEvents for whatever
// changed, per punakawan-panel-implementation-plan.md §12 and §19.
//
// §19 lists four event sources; this package implements only the fourth
// (periodic reconciliation) as the primary mechanism for MVP, not a
// fallback. The panel and any MCP server run as separate OS processes
// with no shared memory, so §19's source 1 ("direct runtime event bus
// when the panel runs in the same process") does not apply to this
// architecture, and real filesystem watching (fsnotify, source 3) is a
// real dependency this phase chooses not to add yet - a short poll
// interval is a simpler, dependency-free way to meet §18's "live update
// visible in UI under 1 second" target for the workspace counts this
// phase actually has readers for.
package events

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// historyLimit bounds Hub's replay buffer, per §18's "bound all list
// responses" - a client that reconnects after being disconnected longer
// than this simply misses the oldest events and should refetch summaries
// itself (§12's frontend behavior: "never assume an SSE event alone
// contains a complete canonical object").
const historyLimit = 500

// clientBuffer is each subscriber channel's capacity. A slow client that
// falls behind has old events dropped for it rather than blocking
// Publish for every other client.
const clientBuffer = 32

// Hub is an in-process pub/sub broadcaster for protocol.PanelEvent.
type Hub struct {
	mu      sync.Mutex
	clients map[chan protocol.PanelEvent]struct{}
	history []protocol.PanelEvent
	nextID  atomic.Uint64
}

// NewHub returns an empty Hub.
func NewHub() *Hub {
	return &Hub{clients: make(map[chan protocol.PanelEvent]struct{})}
}

// Subscribe registers a new client and returns its event channel plus an
// unsubscribe function the caller must call when done (typically via
// defer) to release the channel.
func (h *Hub) Subscribe() (<-chan protocol.PanelEvent, func()) {
	ch := make(chan protocol.PanelEvent, clientBuffer)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
	}
	return ch, unsubscribe
}

// Publish assigns evt a monotonically increasing id (if it does not
// already have one) and broadcasts it to every current subscriber,
// non-blocking: a subscriber whose buffer is full misses the event
// rather than stalling every other subscriber or the publisher.
func (h *Hub) Publish(evt protocol.PanelEvent) protocol.PanelEvent {
	if evt.Id == "" {
		evt.Id = fmt.Sprintf("evt-%d", h.nextID.Add(1))
	}

	h.mu.Lock()
	h.history = append(h.history, evt)
	if len(h.history) > historyLimit {
		h.history = h.history[len(h.history)-historyLimit:]
	}
	for ch := range h.clients {
		select {
		case ch <- evt:
		default:
		}
	}
	h.mu.Unlock()
	return evt
}

// Since returns every recorded event after lastEventID, per SSE's
// Last-Event-ID reconnection semantics (§12). An empty or unrecognized
// lastEventID (including one older than the retained history) returns no
// events - the caller is expected to refetch summaries itself in that
// case, per §12's "never assume an SSE event alone contains a complete
// canonical object."
func (h *Hub) Since(lastEventID string) []protocol.PanelEvent {
	if lastEventID == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	for i, evt := range h.history {
		if evt.Id == lastEventID {
			out := make([]protocol.PanelEvent, len(h.history)-i-1)
			copy(out, h.history[i+1:])
			return out
		}
	}
	return nil
}
