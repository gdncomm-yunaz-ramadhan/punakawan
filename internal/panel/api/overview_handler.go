package api

import (
	"net/http"

	"github.com/ygrip/punakawan/internal/panel"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// Overview is GET /api/v1/overview's response shape, per §11.1's
// documented fields. RecentKnowledgeChanges and AdapterWarnings are left
// for a later phase (Phase 6 owns adapter health; the knowledge event log
// already exists in internal/knowledge/events.go but has no reader
// exposed through internal/panel/contract yet) - this is not a stub, it
// is the honest subset already wired.
type Overview struct {
	ActiveSessions   []protocol.PanelSessionSummary `json:"active_sessions"`
	PendingApprovals []protocol.ApprovalRecord      `json:"pending_approvals"`
	BlockedTasks     int                            `json:"blocked_tasks"`
	WorkspaceHealth  []contract.WorkspaceSummary    `json:"workspace_health"`
	RecentSessions   []protocol.PanelSessionSummary `json:"recent_sessions"`
}

// recentSessionLimit bounds RecentSessions, per §18's "bound all list
// responses."
const recentSessionLimit = 10

// OverviewHandler serves GET /api/v1/overview. It is scoped to the single
// workspace the panel's *app.App was loaded for, until the workspace
// registry (Phase 1's own registry work) is wired into cross-workspace
// aggregation.
func OverviewHandler(readers panel.Readers, workspaceID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		workspaces, err := readers.Workspace.List(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		blockedTasks := 0
		summaries := make([]contract.WorkspaceSummary, 0, len(workspaces))
		for _, ws := range workspaces {
			blockedTasks += ws.BlockedTaskCount
			summaries = append(summaries, ws)
		}

		sessions, err := readers.Session.List(ctx, workspaceID, contract.SessionFilter{})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var active []protocol.PanelSessionSummary
		for _, s := range sessions {
			if isActiveStatus(s.Status) {
				active = append(active, s)
			}
		}
		recent := sessions
		if len(recent) > recentSessionLimit {
			recent = recent[:recentSessionLimit]
		}

		pending, err := readers.Approval.List(ctx, workspaceID, contract.ApprovalFilter{Status: "pending"})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		writeJSON(w, http.StatusOK, Overview{
			ActiveSessions:   active,
			PendingApprovals: pending,
			BlockedTasks:     blockedTasks,
			WorkspaceHealth:  summaries,
			RecentSessions:   recent,
		})
	}
}

func isActiveStatus(status string) bool {
	switch status {
	case "context-building", "awaiting-clarification", "planning", "awaiting-approval", "executing", "reviewing":
		return true
	default:
		return false
	}
}
