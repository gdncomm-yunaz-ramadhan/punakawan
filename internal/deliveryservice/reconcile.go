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
	"log/slog"
	"sort"
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

	// The delivery's own plan, whether its content arrived here to be
	// saved or it names a revision saved earlier. Both end up linked and
	// recorded the same way: a plan named but not linked left the panel's
	// Plans tab empty for a delivery that certainly had one, and left a
	// second start_delivery naming a newer revision changing nothing.
	deliveryPlan := plan.Plan{ID: strings.TrimSpace(req.PlanID), Revision: req.PlanRevision}
	if !req.HighLevelPlan.IsEmpty() {
		saved, err := s.plans.SaveWithKey(ctx, reconcileKey(orchestrationID, "highlevel-plan", req.HighLevelPlan.ReasonForChange), req.HighLevelPlan.toPlan(stableID("delivery-plan", orchestrationID)))
		if err != nil {
			return report, fmt.Errorf("deliveryservice: save high-level plan: %w", err)
		}
		deliveryPlan = saved
	}
	if deliveryPlan.ID != "" {
		if err := s.linkDeliveryPlan(ctx, orchestrationID, deliveryPlan); err != nil {
			return report, err
		}
		report.Plans = append(report.Plans, planRef(deliveryPlan))
	}

	// The clarity the caller stated when it started this delivery. It is
	// recorded through the same call assess_jira_delivery makes, so there
	// is one assessment writer and one place the two legal values live.
	if req.Source != nil && req.Source.Kind == SourceJira && req.Source.Clarity != "" {
		if _, err := s.deliveries.AssessJira(ctx, reconcileKey(orchestrationID, "clarity", req.Source.Clarity),
			resolved.Execution.ID, "", "", req.Source.Clarity, clarityRationale(*req.Source)); err != nil {
			return report, fmt.Errorf("deliveryservice: record requirement clarity: %w", err)
		}
		// An unclear requirement is a question somebody has to answer, so
		// it is carried as one - visible on the delivery and asked
		// wherever the workspace projects delivery events - rather than
		// as a judgement filed away where only a later reader finds it.
		switch req.Source.Clarity {
		case delivery.ClarityNeedsClarification:
			// The rationale is part of the key: a requirement raised as
			// unclear a second time, for a different reason, is a
			// different question and has to be asked again.
			rationale := clarityRationale(*req.Source)
			if err := s.deliveries.OpenClarityQuestion(ctx, reconcileKey(orchestrationID, "clarity-question", req.Source.Key+"|"+rationale),
				orchestrationID, req.Source.Key, rationale); err != nil {
				return report, fmt.Errorf("deliveryservice: open clarity question: %w", err)
			}
		case delivery.ClarityClear:
			if err := s.deliveries.CloseClarityQuestion(ctx, reconcileKey(orchestrationID, "clarity-answered", req.Source.Key),
				orchestrationID, req.Source.Key); err != nil {
				return report, fmt.Errorf("deliveryservice: close clarity question: %w", err)
			}
		}
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

			// Where this project actually is on disk, recorded so every
			// later delivery for it - started from any directory - finds
			// the same tree. A project nobody can locate does not stop
			// the delivery: its lanes are still real work somebody can do
			// by hand, and the reason is reported rather than raised.
			switch path, cloned, err := s.resolveProjectCheckout(ctx, project, draft, req.WorkspaceRoot, false); {
			case err != nil:
				report.Warnings = append(report.Warnings, fmt.Sprintf("project %q: %v - pass its local_path, start a delivery from inside the checkout once, or let punakawan clone it when the lane is given a worktree", draft.Slug, err))
			case cloned:
				report.Checkouts = append(report.Checkouts, fmt.Sprintf("%s -> %s (cloned)", draft.Slug, path))
			default:
				report.Checkouts = append(report.Checkouts, fmt.Sprintf("%s -> %s", draft.Slug, path))
			}

			orch, err := s.deliveries.GetOrchestration(ctx, orchestrationID)
			if err != nil {
				return report, fmt.Errorf("deliveryservice: read orchestration before attach: %w", err)
			}
			if _, err := s.deliveries.AttachProject(ctx, reconcileKey(orchestrationID, "attach", project.Id), orchestrationID, orch.Revision, project.Id); err != nil {
				return report, fmt.Errorf("deliveryservice: attach project %q: %w", draft.Slug, err)
			}
		}

		if firstDraftForSlug && !draft.Plan.IsEmpty() {
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

	report.UncoveredRequirements = uncoveredRequirements(ctx, s, orchestrationID, sources)
	return report, nil
}

// uncoveredRequirements is the inverse of the task-to-source mapping
// above: it walks the captured sources and names the ones no parent task
// covers. The commonest shape is a Jira parent whose subtasks were
// hydrated as requirement sources while the caller's tasks[] named only
// the parent - which is legitimate when the parent is the unit of work,
// and a real omission when it is not. Either way the caller should be
// told, and the readiness check decides what it means at completion.
//
// A read failure here is not allowed to fail the reconciliation that has
// already succeeded: a missing warning is a far smaller harm than losing
// a delivery that was correctly created.
func uncoveredRequirements(ctx context.Context, s *Service, orchestrationID string, sources []*protocol.RequirementSource) []string {
	if len(sources) == 0 {
		return nil
	}
	tasks, _, err := s.deliveries.ListGraph(ctx, orchestrationID)
	if err != nil {
		slog.Warn("deliveryservice: list parent tasks for coverage check", "orchestration_id", orchestrationID, "error", err)
		return nil
	}
	covered := map[string]bool{}
	for _, task := range tasks {
		for _, id := range task.SourceIds {
			covered[id] = true
		}
	}
	var out []string
	for _, src := range sources {
		if src == nil || covered[src.Id] {
			continue
		}
		label := src.CanonicalKey
		if src.ExternalId != nil && strings.TrimSpace(*src.ExternalId) != "" {
			label = strings.TrimSpace(*src.ExternalId)
		}
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

// toPlan turns a not-yet-saved PlanDraft into an actual plan.Plan ready
// for Store.SaveWithKey: id is this lineage's stable identity (see
// clarityRationale is the rationale to record with an assessment. A clear
// requirement need not explain itself, but an assessment row still wants
// a reason, so a plain one stands in.
func clarityRationale(source SourceIdentity) string {
	if rationale := strings.TrimSpace(source.ClarityRationale); rationale != "" {
		return rationale
	}
	return "Stated clear when this delivery was started."
}

// linkDeliveryPlan links one plan revision to the delivery and points the
// orchestration at it, leaving both alone when they already say so.
//
// Pointing the orchestration is what makes a revision visible as the
// delivery's current plan and what emits plan.created/plan.revised - and
// so, in a workspace that asks for it, what says on the Jira issue that
// the plan changed.
func (s *Service) linkDeliveryPlan(ctx context.Context, orchestrationID string, p plan.Plan) error {
	ref := planRef(p)
	if err := s.deliveries.LinkDeliveryPlan(ctx, reconcileKey(orchestrationID, "link-delivery-plan", ref), orchestrationID, p.ID, p.Revision); err != nil {
		return fmt.Errorf("deliveryservice: link delivery plan %s: %w", ref, err)
	}
	orch, err := s.deliveries.GetOrchestration(ctx, orchestrationID)
	if err != nil {
		return fmt.Errorf("deliveryservice: read orchestration before recording plan %s: %w", ref, err)
	}
	if orch.PlanId != nil && *orch.PlanId == p.ID && orch.PlanRevision != nil && *orch.PlanRevision == p.Revision {
		return nil
	}
	planID, planRevision := p.ID, p.Revision
	if _, err := s.deliveries.UpdateOrchestrationDetails(ctx, reconcileKey(orchestrationID, "delivery-plan-details", ref), orchestrationID, orch.Revision, delivery.OrchestrationDetails{
		PlanID: &planID, PlanRevision: &planRevision,
	}); err != nil {
		return fmt.Errorf("deliveryservice: record plan %s on orchestration: %w", ref, err)
	}
	return nil
}

// stableID), and projectIDs is empty for the cross-project high-level
// plan or exactly the one project a detailed plan belongs to.
func (d PlanDraft) toPlan(id string, projectIDs ...string) plan.Plan {
	objective := strings.TrimSpace(d.Objective)
	if objective == "" {
		objective = strings.TrimSpace(d.Title)
	}
	if objective == "" {
		objective = strings.TrimSpace(d.Content)
	}
	if objective == "" {
		objective = "untitled plan"
	}
	return plan.Plan{
		ID: id, ProjectIDs: projectIDs, Objective: objective,
		Steps:              d.Steps,
		AcceptanceCriteria: d.AcceptanceCriteria,
		Verification:       strings.TrimSpace(d.Verification),
		Assumptions:        d.Assumptions,
		ReasonForChange:    strings.TrimSpace(d.ReasonForChange),
		LegacyMarkdown:     d.Content,
	}
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
