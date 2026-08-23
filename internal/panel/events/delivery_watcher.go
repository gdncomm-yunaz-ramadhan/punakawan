package events

import (
	"context"
	"time"

	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// DeliveryWatchWaitSeconds is how long each per-orchestration long-poll
// call blocks server-side before returning, mirroring the daemon's own
// deliveryWatchWaitSeconds (internal/daemon/delivery.go) - the client side
// of the same contract, not an independent tuning knob.
const DeliveryWatchWaitSeconds = 25

// DeliveryDiscoveryInterval is how often DeliveryWatcher re-lists
// deliveries to notice a brand new orchestration to start watching.
// Already-watched orchestrations get pushed to Hub the moment the daemon's
// own long-poll unblocks, so this only needs to be cheap and infrequent -
// it exists solely to pick up orchestrations created after Run started.
const DeliveryDiscoveryInterval = 5 * time.Second

// deliveryWatchErrorBackoff bounds how fast watchOne retries after a
// transport error (daemon briefly unreachable), so a persistent failure
// does not turn into a hot loop.
const deliveryWatchErrorBackoff = 1 * time.Second

// DeliveryWatcher publishes delivery.updated PanelEvents whenever a
// watched orchestration's LatestSeq advances, by long-polling
// Reader.WatchDeliveryView once per still-open orchestration - the panel's
// push-style update mechanism for AC4 ("blocker completion visibly unlocks
// dependents without refresh"), reusing the same NewlyRunnableLaneIDs
// diffing BuildDeliveryViewSince already computes rather than a second
// polling loop over lanes. It is a lightweight parallel mechanism to
// Reconciler rather than a fourth tier on it: long-polling one goroutine
// per orchestration does not fit Reconciler's fixed-tick-then-scan-every-
// entity shape, which assumes a cheap, synchronous, in-process read.
type DeliveryWatcher struct {
	Hub    *Hub
	Reader contract.DeliveryReader
}

// Run discovers deliveries every DeliveryDiscoveryInterval and keeps one
// long-poll goroutine alive per still-open orchestration until ctx is
// cancelled. A nil Reader (no daemon connection at startup) makes this an
// immediate no-op, matching Reconciler's own nil-reader degradation.
// Call it in its own goroutine.
func (w *DeliveryWatcher) Run(ctx context.Context) {
	if w.Reader == nil {
		return
	}
	watching := map[string]context.CancelFunc{}
	defer func() {
		for _, cancel := range watching {
			cancel()
		}
	}()

	w.discover(ctx, watching)
	ticker := time.NewTicker(DeliveryDiscoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.discover(ctx, watching)
		}
	}
}

// discover lists current deliveries, starts a watchOne goroutine for every
// not-yet-watched, still-open orchestration, and stops watching any that
// disappeared (or is no longer listed) since the previous discover.
func (w *DeliveryWatcher) discover(ctx context.Context, watching map[string]context.CancelFunc) {
	list, err := w.Reader.ListDeliveries(ctx)
	if err != nil {
		return
	}
	seen := make(map[string]bool, len(list))
	for _, orch := range list {
		if orch == nil {
			continue
		}
		seen[orch.Id] = true
		if _, ok := watching[orch.Id]; ok || isTerminalDeliveryStatus(orch.Status) {
			continue
		}
		watchCtx, cancel := context.WithCancel(ctx)
		watching[orch.Id] = cancel
		go w.watchOne(watchCtx, orch.Id)
	}
	for id, cancel := range watching {
		if !seen[id] {
			cancel()
			delete(watching, id)
		}
	}
}

// watchOne long-polls one orchestration's DeliveryView until ctx is
// cancelled, publishing delivery.updated to Hub every time LatestSeq
// advances past the last value it saw.
func (w *DeliveryWatcher) watchOne(ctx context.Context, orchestrationID string) {
	sinceSeq := 0
	for {
		view, err := w.Reader.WatchDeliveryView(ctx, orchestrationID, sinceSeq, DeliveryWatchWaitSeconds)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(deliveryWatchErrorBackoff):
			}
			continue
		}
		if view.LatestSeq != sinceSeq {
			sinceSeq = view.LatestSeq
			w.Hub.Publish(protocol.PanelEvent{
				Type:       protocol.PanelEventTypeDeliveryUpdated,
				OccurredAt: time.Now().UTC(),
				EntityId:   strPtr(orchestrationID),
				Payload: protocol.PanelEventPayload{
					"latest_seq":              view.LatestSeq,
					"newly_runnable_lane_ids": view.NewlyRunnableLaneIDs,
				},
			})
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// isTerminalDeliveryStatus reports whether an orchestration is done
// changing, so watchOne need not be started (or kept running) for it.
func isTerminalDeliveryStatus(status protocol.DeliveryOrchestrationStatus) bool {
	switch status {
	case protocol.DeliveryOrchestrationStatusCompleted, protocol.DeliveryOrchestrationStatusCancelled:
		return true
	default:
		return false
	}
}
