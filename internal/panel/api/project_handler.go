package api

import (
	"errors"
	"net/http"

	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/project"
)

// ProjectDetail is GET /api/v1/projects/{projectId}'s response shape: the
// summary (counts, availability, pinned/primary) plus the project's ordered
// metadata and current revision, per the project performance plan §14. The
// embedded ProjectSummary is flattened into the top-level JSON object, so the
// wire shape is exactly ProjectSummary's fields plus "metadata" and
// "revision".
type ProjectDetail struct {
	contract.ProjectSummary
	Metadata []project.MetadataEntry `json:"metadata"`
	Revision int                     `json:"revision"`
}

// projectErrorStatus maps a project read error to an HTTP status. An unknown
// project id (contract.ErrWorkspaceUnavailable, since a project id is a
// workspace id) is the caller's mistake, so it answers 404 rather than 500.
func projectErrorStatus(err error) int {
	if errors.Is(err, contract.ErrWorkspaceUnavailable) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

// ProjectsHandler serves GET /api/v1/projects.
func ProjectsHandler(reader contract.ProjectReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		summaries, err := reader.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": summaries})
	}
}

// ProjectHandler serves GET /api/v1/projects/{projectId}, merging the
// project's summary with its metadata and revision.
func ProjectHandler(reader contract.ProjectReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("projectId")
		summary, err := reader.Summary(r.Context(), id)
		if err != nil {
			writeError(w, projectErrorStatus(err), err)
			return
		}
		p, err := reader.Get(r.Context(), id)
		if err != nil {
			writeError(w, projectErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, ProjectDetail{
			ProjectSummary: summary,
			Metadata:       p.Metadata,
			Revision:       p.Revision,
		})
	}
}
