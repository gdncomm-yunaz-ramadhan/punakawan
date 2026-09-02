// reconcile.go implements Service.StartOrResolve's reconciliation step:
// turning one StartRequest's requirement drafts, high-level plan, and
// per-project drafts into real, idempotent delivery state - projects,
// plans, parent tasks, lanes - so a retry with the same or additively
// new content never duplicates anything it already created.
package deliveryservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// reconcile performs every reconciliation operation StartOrResolve's
// already-resolved lifetime/execution needs, inside orchestrationID.
// Every write below uses a deterministic key derived from stable identity
// (never from req.IdempotencyKey), so calling this twice for the same
// orchestration - whether that is a genuine retry of the exact same
// request or a later call that discovered additional projects/work -
// only ever creates what does not already exist: an already-registered
// project, already-linked plan, or already-routed task is read back
// unchanged rather than treated as an error.
func (s *Service) reconcile(ctx context.Context, req StartRequest, resolved *delivery.ResolvedExecution) (ReconcileReport, error) {
	orchestrationID := resolved.Execution.OrchestrationID
	report := ReconcileReport{Projects: []string{}, Requirements: []string{}, Plans: []string{}, RunnableWork: []string{}}

	if req.Source != nil && req.Source.Kind == SourceJira && s.hydrator == nil {
		// Without a hydrator the delivery's own Jira parent is still
		// captured by StartOrResolveExecution, but its subtasks are not,
		// so any task keyed to a subtask will find nothing to group. Say
		// so here rather than letting that surface later as an
		// unexplained missing lane.
		report.Skipped = append(report.Skipped, "jira hydration: no Jira hydrator configured, so subtasks of the parent issue were not captured")
	}
	if req.Source != nil && req.Source.Kind == SourceJira && s.hydrator != nil {
		// Hydrate runs before StartOrResolve opens req.Session below, so no
		// delivery session exists yet for this call to scope the hydration
		// snapshot to - req.Session.Participant is a free-text participant
		// label, never a delivery_sessions.id, and passing it as sessionID
		// here made every Jira hydration with a participant set fail
		// CaptureJiraSnapshot's session-scope check with ErrScopeMismatch.
		sources, err := s.hydrator.Hydrate(ctx, resolved.Execution.ID, "", req.IdempotencyKey+":hydrate")
		if err != nil {
			// The delivery's own parent issue is already captured by the
			// time this runs; hydration only adds its subtasks. An
			// unreachable or misconfigured Jira should therefore cost the
			// subtasks and be said out loud, not lose a delivery whose
			// projects and lanes this call is about to create.
			report.Skipped = append(report.Skipped, fmt.Sprintf("jira hydration: %v", err))
		}
		for _, src := range sources {
			report.Requirements = append(report.Requirements, src.IssueKey)
		}
	}
	for _, draft := range req.Requirements {
		rs, err := s.deliveries.CaptureRequirement(ctx, reconcileKey(orchestrationID, "requirement", draft.ExternalID+draft.URL), orchestrationID, delivery.SourceInput{
			Provider: draft.Provider, ExternalID: draft.ExternalID, URL: draft.URL, Title: draft.Title, Summary: draft.Summary,
		})
		if err != nil {
			return report, fmt.Errorf("deliveryservice: capture requirement %q: %w", draft.ExternalID, err)
		}
		report.Requirements = append(report.Requirements, rs.Id)
	}

	if strings.TrimSpace(req.HighLevelPlan.Title) != "" || strings.TrimSpace(req.HighLevelPlan.Content) != "" {
		highLevel, err := s.plans.SaveWithKey(ctx, reconcileKey(orchestrationID, "highlevel-plan", ""), req.HighLevelPlan.toPlan(stableID("delivery-plan", orchestrationID)))
		if err != nil {
			return report, fmt.Errorf("deliveryservice: save high-level plan: %w", err)
		}
		if err := s.deliveries.LinkDeliveryPlan(ctx, reconcileKey(orchestrationID, "link-highlevel-plan", ""), orchestrationID, highLevel.ID, highLevel.Revision); err != nil {
			return report, fmt.Errorf("deliveryservice: link high-level plan: %w", err)
		}
		orch, err := s.deliveries.GetOrchestration(ctx, orchestrationID)
		if err != nil {
			return report, fmt.Errorf("deliveryservice: read orchestration before recording high-level plan: %w", err)
		}
		planID, planRevision := highLevel.ID, highLevel.Revision
		if _, err := s.deliveries.UpdateOrchestrationDetails(ctx, reconcileKey(orchestrationID, "highlevel-plan-details", planRef(highLevel)), orchestrationID, orch.Revision, delivery.OrchestrationDetails{
			PlanID: &planID, PlanRevision: &planRevision,
		}); err != nil {
			return report, fmt.Errorf("deliveryservice: record high-level plan on orchestration: %w", err)
		}
		report.Plans = append(report.Plans, planRef(highLevel))
	}

	sources, err := s.deliveries.ListRequirementSources(ctx, orchestrationID)
	if err != nil {
		return report, fmt.Errorf("deliveryservice: list requirement sources: %w", err)
	}
	sourceByIssueKey := map[string]string{}
	for _, src := range sources {
		if src.Provider == protocol.RequirementSourceProviderJira && src.ExternalId != nil {
			sourceByIssueKey[strings.ToUpper(*src.ExternalId)] = src.Id
		}
	}

	// Several drafts may name the same repository - one per unit of work
	// opened there - so the project-level writes run once for the first
	// draft that mentions a slug and are skipped for its siblings. They are
	// all idempotent anyway; the point is to keep the report from listing
	// the same project and plan once per task.
	reconciledSlugs := make(map[string]bool, len(req.Projects))
	for _, draft := range req.Projects {
		project, err := s.deliveries.UpsertProject(ctx, reconcileKey(orchestrationID, "project", draft.Slug), stableID("project", draft.Slug), draft.Slug, draft.RepositoryURL, draft.DefaultBranch)
		if err != nil {
			return report, fmt.Errorf("deliveryservice: upsert project %q: %w", draft.Slug, err)
		}
		firstDraftForSlug := !reconciledSlugs[draft.Slug]
		reconciledSlugs[draft.Slug] = true
		if firstDraftForSlug {
			report.Projects = append(report.Projects, project.Id)

			orch, err := s.deliveries.GetOrchestration(ctx, orchestrationID)
			if err != nil {
				return report, fmt.Errorf("deliveryservice: read orchestration before attach: %w", err)
			}
			if _, err := s.deliveries.AttachProject(ctx, reconcileKey(orchestrationID, "attach", project.Id), orchestrationID, orch.Revision, project.Id); err != nil {
				return report, fmt.Errorf("deliveryservice: attach project %q: %w", draft.Slug, err)
			}
		}

		if firstDraftForSlug && (strings.TrimSpace(draft.Plan.Title) != "" || strings.TrimSpace(draft.Plan.Content) != "") {
			projectPlan, err := s.plans.SaveWithKey(ctx, reconcileKey(orchestrationID, "plan", project.Id), draft.Plan.toPlan(stableID("project-plan", project.Id), project.Id))
			if err != nil {
				return report, fmt.Errorf("deliveryservice: save project plan for %q: %w", draft.Slug, err)
			}
			if err := s.deliveries.LinkProjectPlan(ctx, reconcileKey(orchestrationID, "link-plan", project.Id), orchestrationID, project.Id, projectPlan.ID, projectPlan.Revision); err != nil {
				return report, fmt.Errorf("deliveryservice: link project plan for %q: %w", draft.Slug, err)
			}
			report.Plans = append(report.Plans, planRef(projectPlan))
		}
		if firstDraftForSlug && draft.PlanID != "" {
			if err := s.deliveries.LinkProjectPlan(ctx, reconcileKey(orchestrationID, "link-plan-ref", project.Id), orchestrationID, project.Id, draft.PlanID, draft.PlanRevision); err != nil {
				return report, fmt.Errorf("deliveryservice: link existing project plan for %q: %w", draft.Slug, err)
			}
			report.Plans = append(report.Plans, fmt.Sprintf("%s@%d", draft.PlanID, draft.PlanRevision))
		}

		sourceIDs := sourceIDsForTask(draft, sourceByIssueKey, sources)
		if len(sourceIDs) == 0 {
			// A project can legitimately be registered and attached ahead
			// of any routable work, so this is not an error - but it is
			// the difference between a delivery with lanes and one
			// without, so it is always reported rather than passed over.
			if key := strings.TrimSpace(draft.TaskKey); key != "" {
				report.Skipped = append(report.Skipped, fmt.Sprintf("project %q task %q: %q matches no captured requirement source, so no parent task or lane was created", draft.Slug, draft.Title, key))
			} else {
				report.Skipped = append(report.Skipped, fmt.Sprintf("project %q: registered and attached, but no task key was given so it has no lane", draft.Slug))
			}
			continue
		}
		title := strings.TrimSpace(draft.Title)
		if title == "" {
			title = draft.TaskKey
		}
		task, err := s.deliveries.CreateParentTask(ctx, reconcileKey(orchestrationID, "task", draft.TaskKey), stableID("task", draft.TaskKey), orchestrationID, title, sourceIDs)
		if err != nil {
			return report, fmt.Errorf("deliveryservice: create parent task %q: %w", draft.TaskKey, err)
		}
		task, err = s.deliveries.RouteParentTask(ctx, reconcileKey(orchestrationID, "route", draft.TaskKey), orchestrationID, task.Id, project.Id)
		if err != nil {
			return report, fmt.Errorf("deliveryservice: route parent task %q: %w", draft.TaskKey, err)
		}
		if _, err := s.deliveries.CreateLane(ctx, reconcileKey(orchestrationID, "lane", draft.TaskKey), stableID("lane", draft.TaskKey), orchestrationID, project.Id, task.Id); err != nil {
			return report, fmt.Errorf("deliveryservice: create lane for %q: %w", draft.TaskKey, err)
		}
	}

	// Unlike every other write above, SyncFrontier is documented as cheap
	// and idempotent in effect (a no-op when no lane's state actually needs
	// to change) precisely because it is meant to be called with a fresh
	// key every time - reusing reconcileKey's stable-per-entity key here
	// would make a real, later graph change (e.g. this same call's own
	// newly created lane) silently never sync on a retried orchestration.
	lanes, err := s.deliveries.SyncFrontier(ctx, req.IdempotencyKey+":frontier", orchestrationID)
	if err != nil {
		return report, fmt.Errorf("deliveryservice: sync frontier: %w", err)
	}
	for _, lane := range lanes {
		if lane.Status == protocol.DeliveryLaneStatusRunnable {
			report.RunnableWork = append(report.RunnableWork, lane.Id)
		}
	}
	return report, nil
}

// toPlan turns a not-yet-saved PlanDraft into an actual plan.Plan ready
// for Store.SaveWithKey: id is this lineage's stable identity (see
// stableID), and projectIDs is empty for the cross-project high-level
// plan or exactly the one project a detailed plan belongs to.
func (d PlanDraft) toPlan(id string, projectIDs ...string) plan.Plan {
	objective := strings.TrimSpace(d.Title)
	if objective == "" {
		objective = strings.TrimSpace(d.Content)
	}
	if objective == "" {
		objective = "untitled plan"
	}
	return plan.Plan{ID: id, ProjectIDs: projectIDs, Objective: objective, LegacyMarkdown: d.Content}
}

// planRef renders one saved plan revision as the compact "id@revision"
// string ReconcileReport.Plans reports.
func planRef(p plan.Plan) string {
	return fmt.Sprintf("%s@%d", p.ID, p.Revision)
}

// sourceIDsForTask resolves draft.TaskKey to the one requirement source
// id it names: a Jira issue key (matched case-insensitively against
// every source this orchestration has already captured), or - for a
// caller that already knows a source's id, canonical key, or bare
// external id - any of those forms directly. Empty when nothing matches,
// meaning this project's task/lane is not created yet.
func sourceIDsForTask(draft ProjectDraft, byIssueKey map[string]string, all []*protocol.RequirementSource) []string {
	key := strings.TrimSpace(draft.TaskKey)
	if key == "" {
		return nil
	}
	if id, ok := byIssueKey[strings.ToUpper(key)]; ok {
		return []string{id}
	}
	for _, src := range all {
		if src.Id == key || src.CanonicalKey == key {
			return []string{src.Id}
		}
		if src.ExternalId != nil && *src.ExternalId == key {
			return []string{src.Id}
		}
	}
	return nil
}

// stableID mints a deterministic id from (kind, identity) alone - never
// from req.IdempotencyKey or an orchestration id - so the same project
// slug, task key, or lane always resolves to the same durable id across
// every call that reconciles it, from any delivery.
func stableID(kind, identity string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + identity))
	return "st-" + hex.EncodeToString(sum[:])[:26]
}

// reconcileKey mints the idempotency key for one reconciliation write,
// scoped to (orchestrationID, kind, identity) rather than to
// req.IdempotencyKey: this is what makes a write idempotent forever for
// this exact entity, not merely idempotent within one retried call, and
// what keeps two different orchestrations' identical-looking identities
// (e.g. two ad-hoc deliveries both naming a task "setup") from
// colliding on the same key.
func reconcileKey(orchestrationID, kind, identity string) string {
	return "reconcile:" + orchestrationID + ":" + kind + ":" + identity
}
