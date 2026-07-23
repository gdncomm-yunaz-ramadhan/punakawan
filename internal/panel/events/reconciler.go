package events

import (
	"context"
	"time"

	"github.com/ygrip/punakawan/internal/panel"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// DefaultInterval is how often Reconciler polls, chosen to meet §18's
// "live update visible in UI under 1 second" for typical local corpora
// (§18: up to 20 workspaces, 10,000 sessions) without busy-polling.
const DefaultInterval = 1 * time.Second

// Reconciler periodically polls panel.Readers and publishes a PanelEvent
// to Hub for whatever changed since the previous poll, per §12's source
// 4 ("periodic reconciliation").
type Reconciler struct {
	Hub         *Hub
	Readers     panel.Readers
	WorkspaceID string
	Interval    time.Duration

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
	r.prevSessions = map[string]protocol.PanelSessionSummary{}
	r.prevApprovals = map[string]protocol.ApprovalRecordStatus{}
	r.prevWorkspaces = map[string]protocol.PanelSourceHealthAvailability{}

	r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSystemReady, OccurredAt: time.Now().UTC()})
	r.reconcileOnce(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reconcileOnce(ctx)
		}
	}
}

func (r *Reconciler) reconcileOnce(ctx context.Context) {
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
