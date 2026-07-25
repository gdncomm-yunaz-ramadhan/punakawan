package api

import (
	"errors"
	"net/http"

	"github.com/ygrip/punakawan/internal/artifact"
)

// PlanSummary is one row of GET
// /api/v1/projects/{projectId}/plans. It is the manifest's index fields -
// enough to render a plan list without opening any version file.
type PlanSummary struct {
	ID             string               `json:"id"`
	Title          string               `json:"title"`
	Status         string               `json:"status"`
	CurrentVersion int                  `json:"current_version"`
	RelatedTasks   []string             `json:"related_tasks"`
	DerivedFrom    artifact.Derivations `json:"derived_from"`
}

// planSummaryFromManifest projects a manifest onto the list row shape,
// normalizing a nil RelatedTasks to an empty slice so the JSON is always
// an array.
func planSummaryFromManifest(m *artifact.PlanManifest) PlanSummary {
	tasks := m.RelatedTasks
	if tasks == nil {
		tasks = []string{}
	}
	return PlanSummary{
		ID:             m.ID,
		Title:          m.Title,
		Status:         m.Status,
		CurrentVersion: m.CurrentVersion,
		RelatedTasks:   tasks,
		DerivedFrom:    m.DerivedFrom,
	}
}

// planDetailResponse is GET
// /api/v1/projects/{projectId}/plans/{planId}'s body: the full manifest
// plus, when the plan has a current version on disk, that version's raw
// content. CurrentVersionContent is omitted (not an empty string) when
// the plan has no versions yet, so the two cases are distinguishable.
type planDetailResponse struct {
	Manifest              *artifact.PlanManifest `json:"manifest"`
	CurrentVersionContent *string                `json:"current_version_content,omitempty"`
}

// ListPlansHandler serves GET /api/v1/projects/{projectId}/plans: the
// project-scoped plan index. It resolves the project's PlanStore through
// the injected ProjectStores (any project, not just the startup
// workspace), lists the plan ids, and returns each plan's manifest as a
// summary row - synthesizing a manifest for any plan that has none, so a
// plan created purely through the review/proposal flow still lists.
//
// This is a READ endpoint only. Plan mutation continues to flow through
// the existing review/proposal/accept protocol; Phase 7 adds the
// project-scoped read surface and the ProjectStores resolver the
// integrator will later point that mutation flow at.
func ListPlansHandler(stores *artifact.ProjectStores) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("projectId")
		plans, err := stores.Plans(projectID)
		if err != nil {
			// An unresolvable project id is the caller's mistake, not a
			// server fault - map it to 404, consistent with ProjectHandler.
			writeError(w, http.StatusNotFound, err)
			return
		}
		ids, err := plans.ListPlans()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		items := make([]PlanSummary, 0, len(ids))
		for _, id := range ids {
			m, err := plans.Manifest(id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			items = append(items, planSummaryFromManifest(m))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// PlanHandler serves GET
// /api/v1/projects/{projectId}/plans/{planId}: one plan's manifest and,
// when present, its current version content. An unresolvable project id
// 404s; a plan id with no directory under the project 404s
// (ErrPlanNotFound). Read-only, as with ListPlansHandler.
func PlanHandler(stores *artifact.ProjectStores) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("projectId")
		planID := r.PathValue("planId")
		plans, err := stores.Plans(projectID)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		m, err := plans.Manifest(planID)
		if errors.Is(err, artifact.ErrPlanNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		resp := planDetailResponse{Manifest: m}
		// Best-effort attach of the current version body: a plan with a
		// manifest but no version yet (current.yaml absent) is valid, so a
		// missing current version is not an error - the field is simply
		// omitted.
		if ref, curErr := plans.Current(planID); curErr == nil {
			if content, _, verErr := plans.Version(planID, ref.Version); verErr == nil {
				body := string(content)
				resp.CurrentVersionContent = &body
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
