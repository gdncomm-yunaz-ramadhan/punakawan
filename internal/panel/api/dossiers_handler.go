package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ygrip/punakawan/internal/dossier"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// writeDossierError maps an internal/dossier error to the HTTP status and
// machine code the Change Dossier API documents (plan §37), mirroring
// writeRolesError. A blocking-findings refusal is special-cased: Finalize
// returns a *dossier.BlockingError whose blocker list is surfaced in the 409
// body so the client can show each reason completion was refused.
func writeDossierError(w http.ResponseWriter, err error) {
	var blocking *dossier.BlockingError
	switch {
	case errors.As(err, &blocking):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":    err.Error(),
			"code":     "blocking_findings",
			"blockers": blocking.Blockers,
		})
	case errors.Is(err, dossier.ErrSelfVerification):
		writeCodeError(w, http.StatusConflict, "self_verification", err)
	case errors.Is(err, dossier.ErrClaimNotFound):
		writeCodeError(w, http.StatusNotFound, "claim_not_found", err)
	case errors.Is(err, dossier.ErrIllegalTransition):
		writeCodeError(w, http.StatusConflict, "illegal_transition", err)
	case errors.Is(err, dossier.ErrDossierNotFound):
		writeCodeError(w, http.StatusNotFound, "not_found", err)
	case errors.Is(err, contract.ErrWorkspaceUnavailable):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

// DossiersListHandler serves GET /api/v1/projects/{projectId}/dossiers,
// returning one dossier summary (no claims/evidence) per id.
func DossiersListHandler(reader contract.DossierReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := reader.ListDossiers(r.Context(), r.PathValue("projectId"))
		if err != nil {
			writeDossierError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// DossierCreateHandler serves POST /api/v1/projects/{projectId}/dossiers. The
// body is a ChangeDossier; the server assigns an id when the caller left it
// empty.
func DossierCreateHandler(reader contract.DossierReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := newID("dossier")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var d protocol.ChangeDossier
		// id/version/status are server-managed (dossier.Create defaults status to
		// draft and stamps version); inject them so the strict decode succeeds
		// for a client that POSTs the dossier minus those.
		if err := decodeServerManaged(r.Body, &d, map[string]any{
			"id":      id,
			"version": dossier.SupportedVersion,
			"status":  string(protocol.ChangeDossierStatusDraft),
		}); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
			return
		}
		created, err := reader.CreateDossier(r.Context(), r.PathValue("projectId"), d)
		if err != nil {
			writeDossierError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"dossier": created})
	}
}

// DossierGetHandler serves GET /api/v1/projects/{projectId}/dossiers/{id},
// returning the full dossier plus its claims and evidence.
func DossierGetHandler(reader contract.DossierReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		loaded, err := reader.GetDossier(r.Context(), r.PathValue("projectId"), r.PathValue("id"))
		if err != nil {
			writeDossierError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"dossier":  loaded.Dossier,
			"claims":   loaded.Claims,
			"evidence": loaded.Evidence,
		})
	}
}

// DossierAddClaimHandler serves
// POST /api/v1/projects/{projectId}/dossiers/{id}/claims. The server assigns a
// claim id when the caller left it empty.
func DossierAddClaimHandler(reader contract.DossierReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := newID("claim")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var claim protocol.DossierClaim
		// id and status are server-managed (dossier.AddClaim defaults status to
		// "claimed"); inject them for the strict decode.
		if err := decodeServerManaged(r.Body, &claim, map[string]any{
			"id":     id,
			"status": string(protocol.DossierClaimStatusClaimed),
		}); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
			return
		}
		created, err := reader.AddDossierClaim(r.Context(), r.PathValue("projectId"), r.PathValue("id"), claim)
		if err != nil {
			writeDossierError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"claim": created})
	}
}

// DossierVerifyClaimHandler serves
// POST /api/v1/projects/{projectId}/dossiers/{id}/claims/{claimId}/verify.
func DossierVerifyClaimHandler(reader contract.DossierReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ByRole string `json:"by_role"`
			Note   string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
			return
		}
		claim, err := reader.VerifyDossierClaim(r.Context(), r.PathValue("projectId"), r.PathValue("id"),
			r.PathValue("claimId"), body.ByRole, body.Note)
		if err != nil {
			writeDossierError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"claim": claim})
	}
}

// DossierDisputeClaimHandler serves
// POST /api/v1/projects/{projectId}/dossiers/{id}/claims/{claimId}/dispute.
func DossierDisputeClaimHandler(reader contract.DossierReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ByRole string `json:"by_role"`
			Note   string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
			return
		}
		claim, err := reader.DisputeDossierClaim(r.Context(), r.PathValue("projectId"), r.PathValue("id"),
			r.PathValue("claimId"), body.ByRole, body.Note)
		if err != nil {
			writeDossierError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"claim": claim})
	}
}

// DossierAddEvidenceHandler serves
// POST /api/v1/projects/{projectId}/dossiers/{id}/evidence. The server assigns
// an evidence id when the caller left it empty.
func DossierAddEvidenceHandler(reader contract.DossierReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := newID("evidence")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var ev protocol.DossierEvidence
		// id is server-managed; inject it for the strict decode.
		if err := decodeServerManaged(r.Body, &ev, map[string]any{"id": id}); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
			return
		}
		created, err := reader.AddDossierEvidence(r.Context(), r.PathValue("projectId"), r.PathValue("id"), ev)
		if err != nil {
			writeDossierError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"evidence": created})
	}
}

// DossierFinalizeHandler serves
// POST /api/v1/projects/{projectId}/dossiers/{id}/finalize. A dossier with
// blocking findings is refused with 409 and the blocker list.
func DossierFinalizeHandler(reader contract.DossierReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := reader.FinalizeDossier(r.Context(), r.PathValue("projectId"), id); err != nil {
			writeDossierError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	}
}

// DossierExportMarkdownHandler serves
// GET /api/v1/projects/{projectId}/dossiers/{id}/export.md as text/markdown.
func DossierExportMarkdownHandler(reader contract.DossierReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		md, err := reader.ExportDossierMarkdown(r.Context(), r.PathValue("projectId"), r.PathValue("id"))
		if err != nil {
			writeDossierError(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(md))
	}
}

// DossierExportJSONHandler serves
// GET /api/v1/projects/{projectId}/dossiers/{id}/export.json as application/json.
func DossierExportJSONHandler(reader contract.DossierReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := reader.ExportDossierJSON(r.Context(), r.PathValue("projectId"), r.PathValue("id"))
		if err != nil {
			writeDossierError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
	}
}
