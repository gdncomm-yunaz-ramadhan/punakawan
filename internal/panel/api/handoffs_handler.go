package api

import (
	"errors"
	"net/http"

	"github.com/ygrip/punakawan/internal/handoff"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// writeHandoffError maps an internal/handoff reader error to an HTTP status. A
// refusal to resume a superseded capsule (contract.ErrHandoffSuperseded) is a
// 409, per plan §43's "a superseded capsule must not resume silently".
func writeHandoffError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, contract.ErrHandoffSuperseded):
		writeCodeError(w, http.StatusConflict, "superseded", err)
	case errors.Is(err, contract.ErrWorkspaceUnavailable):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

// HandoffsListHandler serves GET /api/v1/projects/{projectId}/handoffs.
func HandoffsListHandler(reader contract.HandoffReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := reader.ListHandoffs(r.Context(), r.PathValue("projectId"))
		if err != nil {
			writeHandoffError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// HandoffCreateHandler serves POST /api/v1/projects/{projectId}/handoffs. The
// body is a HandoffCapsule; the server assigns an id when the caller left it
// empty.
func HandoffCreateHandler(reader contract.HandoffReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := newID("handoff")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var h protocol.HandoffCapsule
		// id and version are server-managed (handoff.Create stamps version); the
		// client supplies the domain-required fields (objective, current_phase,
		// project_id, run_id). Inject id/version for the strict decode.
		if err := decodeServerManaged(r.Body, &h, map[string]any{
			"id":      id,
			"version": handoff.SupportedVersion,
		}); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
			return
		}
		created, err := reader.CreateHandoff(r.Context(), r.PathValue("projectId"), h)
		if err != nil {
			writeHandoffError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"handoff": created})
	}
}

// HandoffGetHandler serves GET /api/v1/projects/{projectId}/handoffs/{id}.
func HandoffGetHandler(reader contract.HandoffReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h, err := reader.GetHandoff(r.Context(), r.PathValue("projectId"), r.PathValue("id"))
		if err != nil {
			writeHandoffError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"handoff": h})
	}
}

// HandoffValidateHandler serves
// POST /api/v1/projects/{projectId}/handoffs/{id}/validate, returning the §42
// resume verdict.
func HandoffValidateHandler(reader contract.HandoffReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := reader.ValidateHandoff(r.Context(), r.PathValue("projectId"), r.PathValue("id"))
		if err != nil {
			writeHandoffError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":                string(res.Status),
			"changes_since_handoff": res.ChangesSinceHandoff,
			"required_refresh":      res.RequiredRefresh,
		})
	}
}

// HandoffResumeHandler serves
// POST /api/v1/projects/{projectId}/handoffs/{id}/resume, returning the smallest
// necessary resume context. A superseded capsule is refused with 409.
func HandoffResumeHandler(reader contract.HandoffReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, err := reader.ResumeHandoff(r.Context(), r.PathValue("projectId"), r.PathValue("id"))
		if err != nil {
			writeHandoffError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"context": ctx})
	}
}

// HandoffSupersedeHandler serves
// POST /api/v1/projects/{projectId}/handoffs/{id}/supersede.
func HandoffSupersedeHandler(reader contract.HandoffReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h, err := reader.SupersedeHandoff(r.Context(), r.PathValue("projectId"), r.PathValue("id"))
		if err != nil {
			writeHandoffError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"handoff": h})
	}
}
