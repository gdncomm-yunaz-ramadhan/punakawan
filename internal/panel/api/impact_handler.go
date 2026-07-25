package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ygrip/punakawan/internal/panel/contract"
)

// writeImpactError maps an internal/impact reader error to an HTTP status. The
// impact store has no lifecycle sentinels of its own, so the only non-500 case
// is a request for a project this instance does not serve.
func writeImpactError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, contract.ErrWorkspaceUnavailable):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

// ImpactNodesHandler serves GET /api/v1/projects/{projectId}/impact/nodes.
func ImpactNodesHandler(reader contract.ImpactReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodes, err := reader.ImpactNodes(r.Context(), r.PathValue("projectId"))
		if err != nil {
			writeImpactError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": nodes})
	}
}

// ImpactNodeHandler serves
// GET /api/v1/projects/{projectId}/impact/nodes/{nodeId}.
func ImpactNodeHandler(reader contract.ImpactReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.PathValue("nodeId")
		node, ok, err := reader.ImpactNode(r.Context(), r.PathValue("projectId"), nodeID)
		if err != nil {
			writeImpactError(w, err)
			return
		}
		if !ok {
			writeCodeError(w, http.StatusNotFound, "not_found", fmt.Errorf("impact: node %q not found", nodeID))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"node": node})
	}
}

// ImpactQueryHandler serves POST /api/v1/projects/{projectId}/impact/query. The
// subject may be given either as a {type,id} object or a bare subject_id
// string; depth defaults to 1 (direct impact) when unspecified or non-positive.
func ImpactQueryHandler(reader contract.ImpactReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Subject *struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			} `json:"subject"`
			SubjectID string   `json:"subject_id"`
			Depth     int      `json:"depth"`
			Include   []string `json:"include"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
			return
		}
		subjectID := body.SubjectID
		if subjectID == "" && body.Subject != nil {
			subjectID = body.Subject.ID
		}
		if subjectID == "" {
			writeCodeError(w, http.StatusBadRequest, "missing_field", errors.New("subject_id (or subject.id) is required"))
			return
		}
		depth := body.Depth
		if depth <= 0 {
			depth = 1
		}
		result, err := reader.QueryImpact(r.Context(), r.PathValue("projectId"), subjectID, depth, body.Include)
		if err != nil {
			writeImpactError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// ImpactRefreshHandler serves POST /api/v1/projects/{projectId}/impact/refresh,
// re-running the impact builders to reconcile the graph with the workspace.
func ImpactRefreshHandler(reader contract.ImpactReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := reader.RefreshImpact(r.Context(), r.PathValue("projectId")); err != nil {
			writeImpactError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
