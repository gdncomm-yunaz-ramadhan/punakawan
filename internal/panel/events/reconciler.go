package events

import (
	"context"
	"time"

	"github.com/ygrip/punakawan/internal/panel"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// DefaultInterval is how often the fast tier (tier 1) polls, chosen to
// meet §18's "live update visible in UI under 1 second" for typical local
// corpora (§18: up to 20 workspaces, 10,000 sessions) without busy-polling.
const DefaultInterval = 1 * time.Second

// DefaultAvailabilityInterval is how often the slow tier (tier 2) polls
// workspace availability. Per the performance plan §10.2/§10.4, the deep
// Workspace.List inspection (Dolt/bd/git/adapter probes per workspace) is
// far too expensive to run every second: workspace availability changes
// rarely, so it moves to a 15s cadence. Session and approval detection
// stay on DefaultInterval.
const DefaultAvailabilityInterval = 15 * time.Second

// Reconciler periodically polls panel.Readers and publishes a PanelEvent
// to Hub for whatever changed since the previous poll, per §12's source
// 4 ("periodic reconciliation").
//
// Polling is split into two tiers so the cheap, latency-sensitive checks
// (sessions, approvals) run every second while the expensive workspace
// availability inspection runs on a slower cadence:
//
//   - Tier 1 (Interval, default 1s): session + approval change detection.
//   - Tier 2 (AvailabilityInterval, default 15s): workspace availability
//     change detection, which triggers the deep per-workspace probes.
type Reconciler struct {
	Hub         *Hub
	Readers     panel.Readers
	WorkspaceID string
	Interval    time.Duration
	// AvailabilityInterval is tier 2's cadence. Zero selects
	// DefaultAvailabilityInterval.
	AvailabilityInterval time.Duration

	prevSessions   map[string]protocol.PanelSessionSummary
	prevApprovals  map[string]protocol.ApprovalRecordStatus
	prevWorkspaces map[string]protocol.PanelSourceHealthAvailability
}

func strPtr(s string) *string { return &s }

// Run polls until ctx is cancelled. Call it in its own goroutine.
func (r *Reconciler) Run(ctx context.Context) {
	interval := r.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	availInterval := r.AvailabilityInterval
	if availInterval <= 0 {
		availInterval = DefaultAvailabilityInterval
	}
	r.prevSessions = map[string]protocol.PanelSessionSummary{}
	r.prevApprovals = map[string]protocol.ApprovalRecordStatus{}
	r.prevWorkspaces = map[string]protocol.PanelSourceHealthAvailability{}

	r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSystemReady, OccurredAt: time.Now().UTC()})
	// Prime both tiers once so first-sighting events fire immediately.
	r.reconcileFast(ctx)
	r.reconcileAvailability(ctx)

	fastTicker := time.NewTicker(interval)
	defer fastTicker.Stop()
	availTicker := time.NewTicker(availInterval)
	defer availTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-fastTicker.C:
			r.reconcileFast(ctx)
		case <-availTicker.C:
			r.reconcileAvailability(ctx)
		}
	}
}

// reconcileOnce runs both tiers in one pass. Retained for callers/tests
// that want a single synchronous reconciliation of everything.
func (r *Reconciler) reconcileOnce(ctx context.Context) {
	r.reconcileFast(ctx)
	r.reconcileAvailability(ctx)
}

// reconcileFast is tier 1: session + approval change detection. It never
// calls the expensive Workspace.List.
func (r *Reconciler) reconcileFast(ctx context.Context) {
	now := time.Now().UTC()

	if sessions, err := r.Readers.Session.List(ctx, r.WorkspaceID, contract.SessionFilter{}); err == nil {
		seen := make(map[string]bool, len(sessions))
		for _, s := range sessions {
			seen[s.Id] = true
			prev, existed := r.prevSessions[s.Id]
			switch {
			case !existed:
				r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSessionStarted, OccurredAt: now, WorkspaceId: strPtr(s.WorkspaceId), SessionId: strPtr(s.Id)})
			case prev.Status != s.Status:
				r.Hub.Publish(protocol.PanelEvent{Type: sessionStatusEventType(s.Status), OccurredAt: now, WorkspaceId: strPtr(s.WorkspaceId), SessionId: strPtr(s.Id)})
			case prev.UpdatedAt != s.UpdatedAt:
				r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSessionProgress, OccurredAt: now, WorkspaceId: strPtr(s.WorkspaceId), SessionId: strPtr(s.Id)})
			}
			r.prevSessions[s.Id] = s
		}
		for id := range r.prevSessions {
			if !seen[id] {
				delete(r.prevSessions, id)
			}
		}
	}

	if pending, err := r.Readers.Approval.List(ctx, r.WorkspaceID, contract.ApprovalFilter{}); err == nil {
		seen := make(map[string]bool, len(pending))
		for _, a := range pending {
			seen[a.Id] = true
			prevStatus, existed := r.prevApprovals[a.Id]
			if !existed && a.Status == protocol.ApprovalRecordStatusPending {
				r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeApprovalRequested, OccurredAt: now, WorkspaceId: strPtr(r.WorkspaceID), SessionId: strPtr(a.RunId), EntityId: strPtr(a.Id)})
			} else if existed && prevStatus != a.Status && a.Status != protocol.ApprovalRecordStatusPending {
				r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeApprovalResolved, OccurredAt: now, WorkspaceId: strPtr(r.WorkspaceID), SessionId: strPtr(a.RunId), EntityId: strPtr(a.Id)})
			}
			r.prevApprovals[a.Id] = a.Status
		}
		for id := range r.prevApprovals {
			if !seen[id] {
				delete(r.prevApprovals, id)
			}
		}
	}
}

// reconcileAvailability is tier 2: workspace availability change
// detection. Workspace.List is the deep per-workspace probe (Dolt/bd/git/
// adapters) the performance plan §10.2 keeps off the 1s hot path, so this
// runs on the slower AvailabilityInterval cadence.
func (r *Reconciler) reconcileAvailability(ctx context.Context) {
	now := time.Now().UTC()

	if workspaces, err := r.Readers.Workspace.List(ctx); err == nil {
		for _, ws := range workspaces {
			if prev, existed := r.prevWorkspaces[ws.ID]; !existed || prev != ws.Availability {
				r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeWorkspaceAvailabilityChanged, OccurredAt: now, WorkspaceId: strPtr(ws.ID)})
			}
			r.prevWorkspaces[ws.ID] = ws.Availability
		}
	}
}

func sessionStatusEventType(status string) protocol.PanelEventType {
	switch status {
	case "completed":
		return protocol.PanelEventTypeSessionCompleted
	case "failed":
		return protocol.PanelEventTypeSessionFailed
	default:
		return protocol.PanelEventTypeSessionPhaseChanged
	}
}
