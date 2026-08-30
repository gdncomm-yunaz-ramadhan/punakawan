package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/roleconfig"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// writeRolesError maps an internal/roleconfig error to the HTTP status and
// machine code the role prompt-preferences API documents. It mirrors
// writeMetadataError: the machine "code" lets the frontend react without
// string-matching the human message, and a revision conflict additionally
// reports the current revision (fetched fresh) so the client can rebase and
// retry without a second round-trip.
func writeRolesError(w http.ResponseWriter, r *http.Request, reader contract.RolesReader, projectID string, err error) {
	switch {
	case errors.Is(err, roleconfig.ErrRevisionConflict):
		body := map[string]any{"error": err.Error(), "code": "revision_conflict"}
		if cfg, gerr := reader.GetRoles(r.Context(), projectID); gerr == nil {
			body["current_revision"] = cfg.Revision
		}
		writeJSON(w, http.StatusConflict, body)
	case errors.Is(err, roleconfig.ErrUnknownRole):
		writeCodeError(w, http.StatusNotFound, "unknown_role", err)
	case errors.Is(err, roleconfig.ErrInvalidStyle):
		writeCodeError(w, http.StatusBadRequest, "invalid_style", err)
	case errors.Is(err, roleconfig.ErrInstructionsTooLong):
		writeCodeError(w, http.StatusBadRequest, "instructions_too_long", err)
	case errors.Is(err, contract.ErrWorkspaceUnavailable):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

// RolesListHandler serves GET /api/v1/projects/{projectId}/roles. The response
// carries the four-role prompt preferences and their revision.
func RolesListHandler(reader contract.RolesReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("projectId")
		cfg, err := reader.GetRoles(r.Context(), id)
		if err != nil {
			writeRolesError(w, r, reader, id, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"roles": cfg.Roles, "revision": cfg.Revision})
	}
}

// RoleUpdateHandler serves PATCH /api/v1/projects/{projectId}/roles/{role}. A
// missing "style" or "instructions" in the body leaves that field unchanged;
// only the fields present are applied.
func RoleUpdateHandler(reader contract.RolesReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("projectId")
		role := r.PathValue("role")
		var body struct {
			Style        *string `json:"style"`
			Instructions *string `json:"instructions"`
			BaseRevision int     `json:"base_revision"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
			return
		}
		var patch roleconfig.Patch
		if body.Style != nil {
			style := protocol.RolePreferenceStyle(*body.Style)
			patch.Style = &style
		}
		patch.Instructions = body.Instructions
		cfg, err := reader.UpdateRole(r.Context(), id, role, patch, body.BaseRevision)
		if err != nil {
			writeRolesError(w, r, reader, id, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"roles": cfg.Roles, "revision": cfg.Revision})
	}
}

// RoleResetHandler serves POST /api/v1/projects/{projectId}/roles/{role}/reset,
// restoring one role to its recommended defaults under optimistic locking.
func RoleResetHandler(reader contract.RolesReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("projectId")
		role := r.PathValue("role")
		var body struct {
			BaseRevision int `json:"base_revision"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
			return
		}
		cfg, err := reader.ResetRole(r.Context(), id, role, body.BaseRevision)
		if err != nil {
			writeRolesError(w, r, reader, id, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"roles": cfg.Roles, "revision": cfg.Revision})
	}
}
