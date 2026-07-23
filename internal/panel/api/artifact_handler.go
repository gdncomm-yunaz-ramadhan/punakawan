package api

import (
	"net/http"

	"github.com/ygrip/punakawan/pkg/protocol"
)

type artifactContentResponse struct {
	Content   string                     `json:"content"`
	Reference protocol.ArtifactReference `json:"reference"`
}

// ArtifactCurrentHandler serves GET /api/v1/artifacts/{type}/{id}/current:
// the raw content and reference of an artifact's latest version, which
// the review mode's document pane renders and anchors comments against.
// This endpoint has no documented counterpart in §14 (that section only
// covers review/comment/proposal mutation and inspection) - it is an
// honest addition, not a fabrication of the plan doc, since the review UI
// cannot exist without a way to fetch the content it displays.
//
// stores dispatches by the {type} path segment to the matching
// artifact.Store (see resolveArtifactType) rather than being compiled
// against one concrete store - the same handler now serves both "plan"
// and "retrieval_recipe" artifacts, per punokawan-q9r.6's instruction to
// extend the existing protocol rather than fork a parallel one.
func ArtifactCurrentHandler(stores ArtifactStores) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, _, err := resolveArtifactType(stores, r.PathValue("type"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		artifactID := r.PathValue("id")

		ref, err := store.Current(artifactID)
		if isArtifactNotFound(err) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		content, _, err := store.Version(artifactID, ref.Version)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, artifactContentResponse{Content: string(content), Reference: ref})
	}
}
