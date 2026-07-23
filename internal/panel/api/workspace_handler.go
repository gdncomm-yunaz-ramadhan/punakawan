package api

import (
	"net/http"

	"github.com/ygrip/punakawan/internal/panel/contract"
)

// WorkspacesHandler serves GET /api/v1/workspaces.
func WorkspacesHandler(reader contract.WorkspaceReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		summaries, err := reader.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": summaries})
	}
}

// WorkspaceHandler serves GET /api/v1/workspaces/{workspaceId}.
func WorkspaceHandler(reader contract.WorkspaceReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("workspaceId")
		detail, err := reader.Get(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}
