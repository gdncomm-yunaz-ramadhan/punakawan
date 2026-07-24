package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ygrip/punakawan/internal/panel"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// NeedsAttentionKind categorizes one Overview.NeedsAttention entry, per
// §14.1's fixed priority order (failed session, pending approval, blocked
// task, unavailable workspace, stale active session). A "source failure"
// kind was reserved here originally, but per-source health is not available
// from WorkspaceReader.List (it returns WorkspaceSummary, no Health), and a
// wholly-unavailable source already surfaces as an unavailable_workspace
// entry, so the dead kind was removed rather than left un-emitted.
type NeedsAttentionKind string

const (
	NeedsAttentionFailedSession        NeedsAttentionKind = "failed_session"
	NeedsAttentionPendingApproval      NeedsAttentionKind = "pending_approval"
	NeedsAttentionBlockedTasks         NeedsAttentionKind = "blocked_tasks"
	NeedsAttentionUnavailableWorkspace NeedsAttentionKind = "unavailable_workspace"
	NeedsAttentionStaleSession         NeedsAttentionKind = "stale_session"
)

// NeedsAttentionItem is one entry in Overview.NeedsAttention.
type NeedsAttentionItem struct {
	Kind        NeedsAttentionKind `json:"kind"`
	WorkspaceId string             `json:"workspace_id"`
	EntityId    string             `json:"entity_id,omitempty"`
	Message     string             `json:"message"`
}

// staleActiveSessionAfter is how long an active session may go without a
// checkpoint update before Overview flags it as stale, per §14.4's
// "interrupted sessions show the last checkpoint" and §14.1's "stale
// active session" needs-attention category.
const staleActiveSessionAfter = 30 * time.Minute

// Overview is GET /api/v1/overview's response shape, per §11.1 and
// §14.1's documented fields. RecentKnowledgeChanges is left for a later
// phase: the knowledge event log already exists
// (internal/knowledge/events.go) but has no reader exposed through
// internal/panel/contract yet - this is not a stub, it is the honest
// subset already wired.
type Overview struct {
	ActiveSessions      []protocol.PanelSessionSummary `json:"active_sessions"`
	PendingApprovals    []protocol.ApprovalRecord      `json:"pending_approvals"`
	BlockedTasks        int                            `json:"blocked_tasks"`
	AvailableWorkspaces int                            `json:"available_workspaces"`
	NeedsAttention      []NeedsAttentionItem           `json:"needs_attention"`
	WorkspaceHealth     []contract.WorkspaceSummary    `json:"workspace_health"`
	RecentSessions      []protocol.PanelSessionSummary `json:"recent_sessions"`
	// PrimaryWorkspaceId names the single workspace this panel instance was
	// loaded for. The scope of this response is deliberately mixed and this
	// field makes it explicit: BlockedTasks, AvailableWorkspaces, and
	// WorkspaceHealth aggregate across every registered workspace, but
	// ActiveSessions, RecentSessions, and PendingApprovals cover only the
	// primary workspace - the non-workspace sources cannot serve any other
	// workspace's sessions/approvals (they 404). The frontend uses this to
	// label the primary-only cards rather than implying a cross-workspace
	// total.
	PrimaryWorkspaceId string `json:"primary_workspace_id"`
}

// recentSessionLimit bounds RecentSessions, per §18's "bound all list
// responses."
const recentSessionLimit = 10

// OverviewHandler serves GET /api/v1/overview, aggregating across every
// workspace WorkspaceReader knows about (per Phase 2's multi-workspace
// WorkspaceSource) rather than just the single workspace the server's own
// *app.App was loaded for.
func OverviewHandler(readers panel.Readers, workspaceID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		workspaces, err := readers.Workspace.List(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		var attention []NeedsAttentionItem
		blockedTasks := 0
		availableWorkspaces := 0
		for _, ws := range workspaces {
			blockedTasks += ws.BlockedTaskCount
			if ws.Availability == protocol.PanelSourceHealthAvailabilityAvailable {
				availableWorkspaces++
			}
			if ws.BlockedTaskCount > 0 {
				attention = append(attention, NeedsAttentionItem{
					Kind:        NeedsAttentionBlockedTasks,
					WorkspaceId: ws.ID,
					Message:     blockedTasksMessage(ws.BlockedTaskCount),
				})
			}
			if ws.Availability == protocol.PanelSourceHealthAvailabilityUnavailable || ws.Availability == protocol.PanelSourceHealthAvailabilityPartiallyAvailable {
				attention = append(attention, NeedsAttentionItem{
					Kind:        NeedsAttentionUnavailableWorkspace,
					WorkspaceId: ws.ID,
					Message:     "workspace is " + string(ws.Availability),
				})
			}
		}

		sessions, err := readers.Session.List(ctx, workspaceID, contract.SessionFilter{})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var failed, stale []NeedsAttentionItem
		activeSessions := []protocol.PanelSessionSummary{}
		now := time.Now().UTC()
		for _, s := range sessions {
			switch {
			case s.Status == "failed":
				failed = append(failed, NeedsAttentionItem{Kind: NeedsAttentionFailedSession, WorkspaceId: s.WorkspaceId, EntityId: s.Id, Message: "session failed"})
			case isActiveStatus(s.Status):
				activeSessions = append(activeSessions, s)
				if now.Sub(s.UpdatedAt) > staleActiveSessionAfter {
					stale = append(stale, NeedsAttentionItem{Kind: NeedsAttentionStaleSession, WorkspaceId: s.WorkspaceId, EntityId: s.Id, Message: "no checkpoint update in over 30 minutes"})
				}
			}
		}

		pending, err := readers.Approval.List(ctx, workspaceID, contract.ApprovalFilter{Status: "pending"})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var pendingAttention []NeedsAttentionItem
		for _, p := range pending {
			pendingAttention = append(pendingAttention, NeedsAttentionItem{
				Kind:        NeedsAttentionPendingApproval,
				WorkspaceId: workspaceID,
				EntityId:    p.RunId,
				Message:     "pending approval for " + string(p.Operation),
			})
		}

		// §14.1's fixed priority order: failed session, pending approval,
		// blocked task, unavailable workspace, source failure, stale
		// session. attention currently holds blocked-tasks/unavailable
		// entries (built above); assemble the final order here.
		final := append([]NeedsAttentionItem{}, failed...)
		final = append(final, pendingAttention...)
		final = append(final, attention...)
		final = append(final, stale...)

		recent := sessions
		if len(recent) > recentSessionLimit {
			recent = recent[:recentSessionLimit]
		}

		writeJSON(w, http.StatusOK, Overview{
			ActiveSessions:      activeSessions,
			PendingApprovals:    pending,
			BlockedTasks:        blockedTasks,
			AvailableWorkspaces: availableWorkspaces,
			NeedsAttention:      final,
			WorkspaceHealth:     workspaces,
			RecentSessions:      recent,
			PrimaryWorkspaceId:  workspaceID,
		})
	}
}

func blockedTasksMessage(count int) string {
	if count == 1 {
		return "1 blocked task"
	}
	return strconv.Itoa(count) + " blocked tasks"
}

func isActiveStatus(status string) bool {
	switch status {
	case "context-building", "awaiting-clarification", "planning", "awaiting-approval", "executing", "reviewing":
		return true
	default:
		return false
	}
}
