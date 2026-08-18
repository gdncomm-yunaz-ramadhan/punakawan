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
	if errors.Is(err, contract.ErrPrimaryProject) {
		return http.StatusConflict
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

// ProjectDeleteHandler serves DELETE /api/v1/projects/{projectId}: it
// removes the project from this panel's workspace registry so the panel
// stops listing and serving it.
//
// It does NOT delete anything the project owns. The workspace directory,
// its .punakawan tree, knowledge database, tasks, evidence, and every
// repository inside it are left untouched on disk; registering the same
// path again brings the project back. Registry rows carry no revision, so
// this takes no base_revision - unlike the metadata mutations there is no
// concurrent-edit hazard to guard, only presence.
//
// Answers 204 with no body on success, 404 for an unknown project id, and
// 409 (code "primary_project") for the primary workspace, which stays
// resolvable regardless of the registry - so removing it would be a lie.
func ProjectDeleteHandler(reader contract.ProjectReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("projectId")
		if err := reader.Deregister(r.Context(), id); err != nil {
			// The refusal carries a machine-readable code, like the metadata
			// mutations' conflicts, so the panel can word it itself instead of
			// pattern-matching on the status alone.
			if errors.Is(err, contract.ErrPrimaryProject) {
				writeCodeError(w, http.StatusConflict, "primary_project", err)
				return
			}
			writeError(w, projectErrorStatus(err), err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
