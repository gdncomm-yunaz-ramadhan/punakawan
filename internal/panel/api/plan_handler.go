package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/plan"
)

// LinkedDeliveryRef is one delivery that links a plan revision, the
// reverse of "which plan does this delivery use": "which deliveries use
// this plan".
type LinkedDeliveryRef struct {
	OrchestrationID string `json:"orchestration_id"`
	Scope           string `json:"scope"`
	PlanRevision    int    `json:"plan_revision"`
}

// PlanSummary is one row of GET /api/v1/projects/{projectId}/plans: a
// plan lineage's current head plus every delivery that links one of its
// revisions.
type PlanSummary struct {
	ID               string              `json:"id"`
	Objective        string              `json:"objective"`
	Status           string              `json:"status,omitempty"`
	CurrentRevision  int                 `json:"current_revision"`
	ProjectIDs       []string            `json:"project_ids"`
	LinkedDeliveries []LinkedDeliveryRef `json:"linked_deliveries"`
}

func planSummaryFromPlan(p plan.Plan, linked []LinkedDeliveryRef) PlanSummary {
	projectIDs := p.ProjectIDs
	if projectIDs == nil {
		projectIDs = []string{}
	}
	if linked == nil {
		linked = []LinkedDeliveryRef{}
	}
	return PlanSummary{
		ID: p.ID, Objective: p.Objective, Status: p.Status,
		CurrentRevision: p.Revision, ProjectIDs: projectIDs, LinkedDeliveries: linked,
	}
}

// planDetailResponse is GET /api/v1/projects/{projectId}/plans/{planId}'s
// body: one exact plan revision - the lineage's current head, or the
// exact revision named by ?revision= - plus every delivery that links
// this same plan id to this same project.
type planDetailResponse struct {
	Plan             plan.Plan           `json:"plan"`
	LinkedDeliveries []LinkedDeliveryRef `json:"linked_deliveries"`
}

// resolveProject maps the panel's project path segment onto its delivery
// project: project plans are scoped by the delivery project a plan
// names in its own project_ids and links against, not by the panel's
// separate per-workspace registry, so the bridge between the two is one
// exact identity every delivery project already carries - its slug.
func resolveProject(deliveries *delivery.Store, r *http.Request) (string, error) {
	slug := r.PathValue("projectId")
	project, err := deliveries.GetProjectBySlug(r.Context(), slug)
	if err != nil {
		return "", err
	}
	return project.Id, nil
}

func linkedDeliveryRefs(refs []plan.DeliveryPlanRef, planID string) []LinkedDeliveryRef {
	out := []LinkedDeliveryRef{}
	for _, ref := range refs {
		if planID != "" && ref.PlanID != planID {
			continue
		}
		out = append(out, LinkedDeliveryRef{OrchestrationID: ref.OrchestrationID, Scope: ref.Scope, PlanRevision: ref.PlanRevision})
	}
	return out
}

// ListPlansHandler serves GET /api/v1/projects/{projectId}/plans: every
// plan lineage's current head that names this project, each carrying
// every delivery that links one of its revisions. An unresolvable
// project slug 404s, consistent with ProjectHandler.
func ListPlansHandler(deliveries *delivery.Store, plans *plan.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, err := resolveProject(deliveries, r)
		if errors.Is(err, delivery.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		items, err := plans.ListByProject(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		refs, err := plans.ListDeliveriesByProject(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		byPlan := map[string][]LinkedDeliveryRef{}
		for _, ref := range refs {
			byPlan[ref.PlanID] = append(byPlan[ref.PlanID], LinkedDeliveryRef{OrchestrationID: ref.OrchestrationID, Scope: ref.Scope, PlanRevision: ref.PlanRevision})
		}
		summaries := make([]PlanSummary, 0, len(items))
		for _, p := range items {
			summaries = append(summaries, planSummaryFromPlan(p, byPlan[p.ID]))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": summaries})
	}
}

// PlanHandler serves GET /api/v1/projects/{projectId}/plans/{planId}:
// one exact plan revision plus every delivery that links this plan to
// this project. Omitting ?revision= serves the lineage's current head;
// naming one serves exactly that revision - a delivery detail page links
// here with the exact revision it itself links, so the same plan never
// silently renders a later revision than the one the delivery actually
// used. An unresolvable project slug or unknown plan id/revision 404s.
func PlanHandler(deliveries *delivery.Store, plans *plan.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, err := resolveProject(deliveries, r)
		if errors.Is(err, delivery.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		planID := r.PathValue("planId")

		var got plan.Plan
		if raw := r.URL.Query().Get("revision"); raw != "" {
			revision, convErr := strconv.Atoi(raw)
			if convErr != nil {
				writeError(w, http.StatusBadRequest, fmt.Errorf("invalid revision %q", raw))
				return
			}
			got, err = plans.GetRevision(r.Context(), planID, revision)
		} else {
			got, err = plans.Get(r.Context(), planID)
		}
		if errors.Is(err, plan.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		// A plan naming no project at all is an intentional cross-project
		// plan and stays visible from every project; one that does name
		// projects but not this one does not belong on this project's page.
		if len(got.ProjectIDs) > 0 {
			belongs := false
			for _, id := range got.ProjectIDs {
				if id == projectID {
					belongs = true
					break
				}
			}
			if !belongs {
				writeError(w, http.StatusNotFound, fmt.Errorf("plan %q does not belong to project %q", planID, r.PathValue("projectId")))
				return
			}
		}

		refs, err := plans.ListDeliveriesByProject(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, planDetailResponse{Plan: got, LinkedDeliveries: linkedDeliveryRefs(refs, planID)})
	}
}
