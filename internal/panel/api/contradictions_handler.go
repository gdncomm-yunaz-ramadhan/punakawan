package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ygrip/punakawan/internal/contradiction"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// writeContradictionError maps an internal/contradiction error to the HTTP
// status and machine code the Contradiction Ledger API documents (plan §21),
// mirroring writeRolesError: the machine "code" lets the frontend react without
// string-matching the human message. A missing record is 404; an illegal
// lifecycle transition (§18 DAG) is 409.
func writeContradictionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, contradiction.ErrNotFound):
		writeCodeError(w, http.StatusNotFound, "not_found", err)
	case errors.Is(err, contradiction.ErrIllegalTransition):
		writeCodeError(w, http.StatusConflict, "illegal_transition", err)
	case errors.Is(err, contract.ErrWorkspaceUnavailable):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

// ContradictionsListHandler serves
// GET /api/v1/projects/{projectId}/contradictions.
func ContradictionsListHandler(reader contract.ContradictionReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := reader.ListContradictions(r.Context(), r.PathValue("projectId"))
		if err != nil {
			writeContradictionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// ContradictionGetHandler serves
// GET /api/v1/projects/{projectId}/contradictions/{id}.
func ContradictionGetHandler(reader contract.ContradictionReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := reader.GetContradiction(r.Context(), r.PathValue("projectId"), r.PathValue("id"))
		if err != nil {
			writeContradictionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"contradiction": c})
	}
}

// ContradictionCreateHandler serves
// POST /api/v1/projects/{projectId}/contradictions. The body is a Contradiction
// minus server-managed fields; the server assigns an id when the caller left it
// empty, then persists through contradiction.Put.
func ContradictionCreateHandler(reader contract.ContradictionReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := newID("con")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var c protocol.Contradiction
		// id and version are server-managed; the client POSTs the record minus
		// those, so inject them before the strict decode (the store re-stamps
		// version and preserves a client-supplied id when present).
		if err := decodeServerManaged(r.Body, &c, map[string]any{
			"id":      id,
			"version": contradiction.Version,
		}); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
			return
		}
		created, err := reader.CreateContradiction(r.Context(), r.PathValue("projectId"), c)
		if err != nil {
			writeContradictionError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"contradiction": created})
	}
}

// ContradictionProposeResolutionHandler serves
// POST /api/v1/projects/{projectId}/contradictions/{id}/propose-resolution.
func ContradictionProposeResolutionHandler(reader contract.ContradictionReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ProposedStatement         string `json:"proposed_statement"`
			Rationale                 string `json:"rationale"`
			RequiresHumanConfirmation bool   `json:"requires_human_confirmation"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
			return
		}
		c, err := reader.ProposeContradictionResolution(r.Context(), r.PathValue("projectId"), r.PathValue("id"),
			body.ProposedStatement, body.Rationale, body.RequiresHumanConfirmation)
		if err != nil {
			writeContradictionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"contradiction": c})
	}
}

// ContradictionResolveHandler serves
// POST /api/v1/projects/{projectId}/contradictions/{id}/resolve.
func ContradictionResolveHandler(reader contract.ContradictionReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Statement string `json:"statement"`
			By        string `json:"by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
			return
		}
		c, err := reader.ResolveContradiction(r.Context(), r.PathValue("projectId"), r.PathValue("id"),
			body.Statement, body.By)
		if err != nil {
			writeContradictionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"contradiction": c})
	}
}

// ContradictionAcceptDivergenceHandler serves
// POST /api/v1/projects/{projectId}/contradictions/{id}/accept-divergence.
func ContradictionAcceptDivergenceHandler(reader contract.ContradictionReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			By string `json:"by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
			return
		}
		c, err := reader.AcceptContradictionDivergence(r.Context(), r.PathValue("projectId"), r.PathValue("id"), body.By)
		if err != nil {
			writeContradictionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"contradiction": c})
	}
}
