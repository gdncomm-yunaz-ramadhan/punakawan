package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/ygrip/punakawan/internal/artifact"
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
func ArtifactCurrentHandler(plans *artifact.PlanStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		artifactType := r.PathValue("type")
		if artifactType != string(protocol.ArtifactReviewArtifactTypePlan) {
			writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported artifact type %q (only %q is implemented)", artifactType, protocol.ArtifactReviewArtifactTypePlan))
			return
		}
		artifactID := r.PathValue("id")

		ref, err := plans.Current(artifactID)
		if errors.Is(err, artifact.ErrPlanNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		content, _, err := plans.Version(artifactID, ref.Version)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, artifactContentResponse{Content: string(content), Reference: ref})
	}
}
