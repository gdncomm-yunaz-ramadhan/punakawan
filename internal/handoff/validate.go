package handoff

import "fmt"

// ResumeStatus is the §42 classification of whether a capsule can be resumed.
type ResumeStatus string

const (
	// StatusResumable: every checked precondition still holds; resume directly.
	StatusResumable ResumeStatus = "resumable"
	// StatusRefreshRequired: the work is still valid but some inputs moved
	// (role config or contradictions changed, task no longer current); the
	// resumer must refresh the listed items before continuing.
	StatusRefreshRequired ResumeStatus = "refresh_required"
	// StatusBlocked: a referenced object the work depends on is gone (plan
	// version or evidence missing); resume is impossible until it is restored.
	StatusBlocked ResumeStatus = "blocked"
	// StatusSuperseded: the capsule (or its dossier) was superseded; it must
	// not resume silently.
	StatusSuperseded ResumeStatus = "superseded"
	// StatusInvalid: the recorded world no longer matches reality (repository
	// state diverged); the capsule cannot be trusted to resume.
	StatusInvalid ResumeStatus = "invalid"
)

// ValidationDeps injects the lookups §42 requires, so this package needs no
// dependency on the plan store, role config, contradiction registry, git, or
// evidence packages. Every func is optional: a nil func means "cannot check
// this precondition", which is treated as passing rather than failing, so a
// caller can validate against only the subsystems it has wired.
type ValidationDeps struct {
	// PlanVersionExists reports whether the accepted plan still has the pinned
	// version. Called only when the capsule pins an accepted plan.
	PlanVersionExists func(planID string, version int) (bool, error)
	// RoleConfigRevision returns the project's current role-config revision, to
	// compare against the revision the capsule was created under.
	RoleConfigRevision func() (int, error)
	// TaskIsCurrent reports whether the capsule's current task is still the
	// current one.
	TaskIsCurrent func(taskID string) (bool, error)
	// ContradictionsChanged returns the subset of the given contradiction ids
	// that have materially changed since the capsule was created.
	ContradictionsChanged func(ids []string) ([]string, error)
	// RepositoryStateMatches reports whether the given repositories' state
	// still matches what the capsule recorded.
	RepositoryStateMatches func(repos []string) (bool, error)
	// EvidenceExists returns the subset of the given evidence ids that are
	// MISSING (do not exist).
	EvidenceExists func(ids []string) ([]string, error)
	// DossierSuperseded reports whether the capsule's dossier was superseded.
	DossierSuperseded func(id string) (bool, error)
}

// ValidationResult is §42's resume verdict: the classification plus, for a
// refresh_required verdict, the human-readable list of what changed and the
// concrete refresh steps the resumer must take first.
type ValidationResult struct {
	Status              ResumeStatus
	ChangesSinceHandoff []string
	RequiredRefresh     []string
}

// Validate classifies whether the capsule id can be resumed, per §42. The
// precedence is deliberate and severity-ordered: superseded (never resume
// silently) beats a hard block/invalidation (a dependency is gone or the world
// diverged) which beats refresh_required (still resumable after refreshing
// moved inputs) which beats resumable. Checks whose dep func is nil are
// skipped, so a partially-wired caller still gets a meaningful verdict.
func Validate(root, id string, deps ValidationDeps) (ValidationResult, error) {
	h, err := Get(root, id)
	if err != nil {
		return ValidationResult{}, err
	}

	// 1. Superseded: the capsule itself, or its dossier.
	if h.Superseded != nil && *h.Superseded {
		return ValidationResult{Status: StatusSuperseded}, nil
	}
	if deps.DossierSuperseded != nil && h.Dossier != nil && h.Dossier.Id != nil {
		superseded, err := deps.DossierSuperseded(*h.Dossier.Id)
		if err != nil {
			return ValidationResult{}, fmt.Errorf("handoff: check dossier superseded: %w", err)
		}
		if superseded {
			return ValidationResult{Status: StatusSuperseded}, nil
		}
	}

	// 2. Invalid: repository state diverged from what was recorded. This is the
	//    most severe non-superseded verdict because it means the capsule's view
	//    of the working trees is no longer true.
	if deps.RepositoryStateMatches != nil && len(h.ChangedRepositories) > 0 {
		matches, err := deps.RepositoryStateMatches(h.ChangedRepositories)
		if err != nil {
			return ValidationResult{}, fmt.Errorf("handoff: check repository state: %w", err)
		}
		if !matches {
			return ValidationResult{
				Status:              StatusInvalid,
				ChangesSinceHandoff: []string{"repository state no longer matches the capsule"},
			}, nil
		}
	}

	// 3. Blocked: a referenced dependency is gone.
	var blockers []string
	if deps.PlanVersionExists != nil && h.AcceptedPlan != nil &&
		h.AcceptedPlan.Id != nil && h.AcceptedPlan.Version != nil {
		exists, err := deps.PlanVersionExists(*h.AcceptedPlan.Id, *h.AcceptedPlan.Version)
		if err != nil {
			return ValidationResult{}, fmt.Errorf("handoff: check plan version: %w", err)
		}
		if !exists {
			blockers = append(blockers, fmt.Sprintf("plan %q version %d no longer exists",
				*h.AcceptedPlan.Id, *h.AcceptedPlan.Version))
		}
	}
	if deps.EvidenceExists != nil && len(h.Evidence) > 0 {
		missing, err := deps.EvidenceExists(h.Evidence)
		if err != nil {
			return ValidationResult{}, fmt.Errorf("handoff: check evidence: %w", err)
		}
		for _, m := range missing {
			blockers = append(blockers, fmt.Sprintf("evidence %q no longer exists", m))
		}
	}
	if len(blockers) > 0 {
		return ValidationResult{Status: StatusBlocked, ChangesSinceHandoff: blockers}, nil
	}

	// 4. Refresh required: inputs moved but the work is still resumable once the
	//    resumer reloads them.
	var changes, refresh []string
	if deps.RoleConfigRevision != nil && h.RoleConfigurationRevision != nil {
		cur, err := deps.RoleConfigRevision()
		if err != nil {
			return ValidationResult{}, fmt.Errorf("handoff: check role config revision: %w", err)
		}
		if cur != *h.RoleConfigurationRevision {
			changes = append(changes, fmt.Sprintf("role configuration changed from revision %d to %d",
				*h.RoleConfigurationRevision, cur))
			refresh = append(refresh, "reload role configuration")
		}
	}
	if deps.ContradictionsChanged != nil && len(h.OpenContradictions) > 0 {
		changed, err := deps.ContradictionsChanged(h.OpenContradictions)
		if err != nil {
			return ValidationResult{}, fmt.Errorf("handoff: check contradictions: %w", err)
		}
		if len(changed) > 0 {
			for _, c := range changed {
				changes = append(changes, fmt.Sprintf("%s materially changed", c))
			}
			refresh = append(refresh, "refresh contradiction summary")
		}
	}
	if deps.TaskIsCurrent != nil && h.CurrentTask != nil && h.CurrentTask.Id != nil {
		current, err := deps.TaskIsCurrent(*h.CurrentTask.Id)
		if err != nil {
			return ValidationResult{}, fmt.Errorf("handoff: check current task: %w", err)
		}
		if !current {
			changes = append(changes, fmt.Sprintf("current task %q is no longer current", *h.CurrentTask.Id))
			refresh = append(refresh, "reconfirm current task")
		}
	}
	if len(changes) > 0 {
		return ValidationResult{
			Status:              StatusRefreshRequired,
			ChangesSinceHandoff: changes,
			RequiredRefresh:     refresh,
		}, nil
	}

	// 5. All clear.
	return ValidationResult{Status: StatusResumable}, nil
}

// ResumeContext returns the smallest necessary verified context for resuming
// the capsule (HANDOFF-005 / §43's "only the smallest necessary verified
// context"): objective, current phase, current task and its next action, the
// accepted plan reference, open contradictions, and unresolved risks. It
// returns references (ids and short fields), never copies of plans, evidence,
// or diffs, and omits the bulky capsule fields (completed tasks, changed
// repositories, evidence list, impact summary) that a resumer can fetch on
// demand. Keys are omitted entirely when the capsule leaves them unset, so the
// map stays as small as the capsule allows.
func ResumeContext(root, id string) (map[string]any, error) {
	h, err := Get(root, id)
	if err != nil {
		return nil, err
	}

	ctx := map[string]any{
		"objective":     h.Objective.Statement,
		"current_phase": h.CurrentPhase,
	}
	if len(h.Objective.SourceRefs) > 0 {
		ctx["source_refs"] = h.Objective.SourceRefs
	}
	if h.CurrentTask != nil {
		task := map[string]any{}
		if h.CurrentTask.Id != nil {
			task["id"] = *h.CurrentTask.Id
		}
		if h.CurrentTask.NextAction != nil {
			task["next_action"] = *h.CurrentTask.NextAction
		}
		if len(task) > 0 {
			ctx["current_task"] = task
		}
	}
	if h.AcceptedPlan != nil && (h.AcceptedPlan.Id != nil || h.AcceptedPlan.Version != nil) {
		plan := map[string]any{}
		if h.AcceptedPlan.Id != nil {
			plan["id"] = *h.AcceptedPlan.Id
		}
		if h.AcceptedPlan.Version != nil {
			plan["version"] = *h.AcceptedPlan.Version
		}
		ctx["accepted_plan"] = plan
	}
	if len(h.OpenContradictions) > 0 {
		ctx["open_contradictions"] = h.OpenContradictions
	}
	if len(h.UnresolvedRisks) > 0 {
		ctx["unresolved_risks"] = h.UnresolvedRisks
	}
	return ctx, nil
}
