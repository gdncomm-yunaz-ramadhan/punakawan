package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/project"
)

// writeMetadataError maps an internal/project error to the HTTP status and
// machine code the metadata API documents, per the project performance plan
// §4.3. The machine "code" lets the frontend react without string-matching
// the human message. A revision conflict additionally reports the current
// revision (fetched fresh) so the client can rebase and retry without a
// second round-trip.
func writeMetadataError(w http.ResponseWriter, r *http.Request, reader contract.ProjectReader, projectID string, err error) {
	switch {
	case errors.Is(err, project.ErrRevisionConflict):
		body := map[string]any{"error": err.Error(), "code": "revision_conflict"}
		if p, gerr := reader.Get(r.Context(), projectID); gerr == nil {
			body["revision"] = p.Revision
		}
		writeJSON(w, http.StatusConflict, body)
	case errors.Is(err, project.ErrDuplicateKey):
		writeCodeError(w, http.StatusBadRequest, "duplicate_key", err)
	case errors.Is(err, project.ErrSecretRejected):
		writeCodeError(w, http.StatusBadRequest, "secret_rejected", err)
	case errors.Is(err, project.ErrInvalidValue):
		writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
	case errors.Is(err, project.ErrMissingField):
		writeCodeError(w, http.StatusBadRequest, "missing_field", err)
	case errors.Is(err, project.ErrKeyNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, contract.ErrWorkspaceUnavailable):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

// writeCodeError writes {"error": ..., "code": ...}, extending writeError's
// minimal body with the machine code metadata validation failures carry.
func writeCodeError(w http.ResponseWriter, status int, code string, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error(), "code": code})
}

// MetadataListHandler serves GET /api/v1/projects/{projectId}/metadata.
func MetadataListHandler(reader contract.ProjectReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := reader.Get(r.Context(), r.PathValue("projectId"))
		if err != nil {
			writeError(w, projectErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": p.Metadata, "revision": p.Revision})
	}
}

// MetadataCreateHandler serves POST /api/v1/projects/{projectId}/metadata.
func MetadataCreateHandler(reader contract.ProjectReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("projectId")
		var body struct {
			Key          string `json:"key"`
			Description  string `json:"description"`
			Value        any    `json:"value"`
			BaseRevision int    `json:"base_revision"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
			return
		}
		entry := project.MetadataEntry{Key: body.Key, Description: body.Description, Value: body.Value}
		p, err := reader.AddMetadata(r.Context(), id, entry, body.BaseRevision)
		if err != nil {
			writeMetadataError(w, r, reader, id, err)
			return
		}
		created, _ := p.MetadataFor(body.Key)
		writeJSON(w, http.StatusCreated, map[string]any{"entry": created, "revision": p.Revision})
	}
}

// MetadataUpdateHandler serves PATCH /api/v1/projects/{projectId}/metadata/{key}.
// A missing "description" or "value" in the body leaves that field unchanged;
// only the fields present are applied.
func MetadataUpdateHandler(reader contract.ProjectReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("projectId")
		key := r.PathValue("key")
		var body struct {
			Description  *string         `json:"description"`
			Value        json.RawMessage `json:"value"`
			BaseRevision int             `json:"base_revision"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
			return
		}
		var newValue any
		if body.Value != nil {
			if err := json.Unmarshal(body.Value, &newValue); err != nil {
				writeCodeError(w, http.StatusBadRequest, "invalid_value", err)
				return
			}
		}
		p, err := reader.UpdateMetadata(r.Context(), id, key, body.Description, newValue, body.BaseRevision)
		if err != nil {
			writeMetadataError(w, r, reader, id, err)
			return
		}
		updated, _ := p.MetadataFor(key)
		writeJSON(w, http.StatusOK, map[string]any{"entry": updated, "revision": p.Revision})
	}
}

// MetadataDeleteHandler serves
// DELETE /api/v1/projects/{projectId}/metadata/{key}?base_revision=N.
func MetadataDeleteHandler(reader contract.ProjectReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("projectId")
		key := r.PathValue("key")
		raw := r.URL.Query().Get("base_revision")
		if raw == "" {
			writeCodeError(w, http.StatusBadRequest, "missing_field", errors.New("base_revision query parameter is required"))
			return
		}
		baseRevision, err := strconv.Atoi(raw)
		if err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid_value", errors.New("base_revision must be an integer"))
			return
		}
		if _, err := reader.DeleteMetadata(r.Context(), id, key, baseRevision); err != nil {
			writeMetadataError(w, r, reader, id, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
