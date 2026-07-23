package api

import (
	"net/http"
	"strconv"

	"github.com/ygrip/punakawan/internal/panel/contract"
)

// SessionsHandler serves GET /api/v1/workspaces/{workspaceId}/sessions,
// per §11.3. Supported filters: status, workflow, role, limit. task_id
// and repository_id are accepted by contract.SessionFilter but this
// phase's SessionSource does not yet index sessions by either, so they
// are parsed but not yet applied; date_from/date_to and cursor pagination
// are not implemented yet (no run count in current local corpora needs
// them, per §18's up-to-10,000-sessions target being for later,
// larger-scale hardening).
func SessionsHandler(reader contract.SessionReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter := contract.SessionFilter{
			Status:   r.URL.Query().Get("status"),
			Workflow: r.URL.Query().Get("workflow"),
			Role:     r.URL.Query().Get("role"),
			TaskID:   r.URL.Query().Get("task_id"),
		}
		if limit, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil {
			filter.Limit = limit
		}

		sessions, err := reader.List(r.Context(), r.PathValue("workspaceId"), filter)
		if err != nil {
			writeError(w, listErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": sessions})
	}
}

// SessionHandler serves GET /api/v1/workspaces/{workspaceId}/sessions/{sessionId}.
func SessionHandler(reader contract.SessionReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		detail, err := reader.Get(r.Context(), r.PathValue("workspaceId"), r.PathValue("sessionId"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}
