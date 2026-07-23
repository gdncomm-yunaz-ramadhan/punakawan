package api

import (
	"net/http"
	"strconv"

	"github.com/ygrip/punakawan/internal/panel/contract"
)

// EvidenceListHandler serves
// GET /api/v1/workspaces/{workspaceId}/sessions/{sessionId}/evidence,
// per §14.7's evidence list.
func EvidenceListHandler(reader contract.EvidenceReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := reader.List(r.Context(), r.PathValue("workspaceId"), r.PathValue("sessionId"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// EvidenceHandler serves
// GET /api/v1/workspaces/{workspaceId}/evidence/{evidenceId}: the
// EvidenceRecord's metadata only (path, hash, type) - no artifact bytes.
func EvidenceHandler(reader contract.EvidenceReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec, err := reader.Get(r.Context(), r.PathValue("workspaceId"), r.PathValue("evidenceId"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, rec)
	}
}

// EvidencePreviewHandler serves
// GET /api/v1/workspaces/{workspaceId}/evidence/{evidenceId}/preview,
// optionally bounded by ?offset=&limit= (bytes), per §14.7's ranged log
// loading, diff summaries, and screenshot previews. A "binary" preview
// (screenshot, playwright trace) is written as the raw content type;
// everything else is redacted text returned as JSON.
func EvidencePreviewHandler(reader contract.EvidenceReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var offset, limit int64
		if v, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64); err == nil {
			offset = v
		}
		if v, err := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 64); err == nil {
			limit = v
		}

		preview, err := reader.Preview(r.Context(), r.PathValue("workspaceId"), r.PathValue("evidenceId"), offset, limit)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}

		if preview.Kind == "binary" {
			w.Header().Set("Content-Type", preview.MimeType)
			w.Header().Set("Content-Length", strconv.Itoa(len(preview.Data)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(preview.Data)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"content_type": preview.MimeType,
			"text":         string(preview.Data),
			"offset":       preview.Offset,
			"total_size":   preview.TotalSize,
			"truncated":    preview.Truncated,
			"diff_summary": preview.DiffSummary,
		})
	}
}
