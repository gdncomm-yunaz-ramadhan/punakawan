package events

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// SSEHandler serves GET /api/v1/events, per §12: a text/event-stream of
// hub's PanelEvents. It replays events since the request's Last-Event-ID
// header (or query parameter, for the one case a browser EventSource
// cannot set a custom header: a fresh connection has no prior state to
// resume from anyway, so this only matters for callers using a plain
// fetch-based reconnect) before streaming new events live.
func SSEHandler(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("Connection", "keep-alive")
		h.Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		lastEventID := r.Header.Get("Last-Event-ID")
		if lastEventID == "" {
			lastEventID = r.URL.Query().Get("last_event_id")
		}

		ch, unsubscribe := hub.Subscribe()
		defer unsubscribe()

		if lastEventID == "" {
			// A brand-new connection (no Last-Event-ID to resume from)
			// gets its own system.ready, per §12: the frontend uses this
			// to flip its connection indicator. This is written directly
			// to this one connection rather than published through Hub,
			// so it does not appear in Since() replay for other clients
			// or pollute the shared history with one-per-connection noise.
			if !writeEvent(w, protocol.PanelEvent{Id: "ready", Type: protocol.PanelEventTypeSystemReady, OccurredAt: time.Now().UTC()}) {
				return
			}
		} else {
			for _, evt := range hub.Since(lastEventID) {
				if !writeEvent(w, evt) {
					return
				}
			}
		}
		flusher.Flush()

		for {
			select {
			case <-r.Context().Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				if !writeEvent(w, evt) {
					return
				}
				flusher.Flush()
			}
		}
	}
}

func writeEvent(w http.ResponseWriter, evt protocol.PanelEvent) bool {
	data, err := json.Marshal(evt)
	if err != nil {
		return true // skip a malformed event rather than killing the stream
	}
	_, err1 := w.Write([]byte("id: " + evt.Id + "\n"))
	_, err2 := w.Write([]byte("event: " + string(evt.Type) + "\n"))
	_, err3 := w.Write([]byte("data: "))
	_, err4 := w.Write(data)
	_, err5 := w.Write([]byte("\n\n"))
	return err1 == nil && err2 == nil && err3 == nil && err4 == nil && err5 == nil
}
