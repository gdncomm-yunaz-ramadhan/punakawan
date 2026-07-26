package workflow

import (
	"fmt"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// StepDef is the minimal definition-side view the step engine needs. The
// caller (which holds the full workflowdef.Definition) projects the steps into
// this shape, keeping internal/workflow decoupled from internal/workflowdef.
type StepDef struct {
	ID         string
	Capability string
	Intent     string
	InputFrom  []string
}

// StepView is a read model of one step's current execution state, returned by
// NextSteps for get_next_workflow_step.
type StepView struct {
	StepID     string `json:"step_id"`
	Capability string `json:"capability"`
	Intent     string `json:"intent,omitempty"`
	State      string `json:"state"`
	Reason     string `json:"reason,omitempty"`
}

// AllowedFunc reports whether a capability may be used by this run: it is both
// in the workflow's allowlist and registered on the server. The caller
// composes the definition allowlist with the capability registry.
type AllowedFunc func(capability string) bool

// stepState indexes a run's step_progress by step id.
func stepState(run protocol.WorkflowRun) map[string]protocol.WorkflowRunStepProgressElemState {
	m := make(map[string]protocol.WorkflowRunStepProgressElemState, len(run.StepProgress))
	for _, sp := range run.StepProgress {
		m[sp.StepId] = sp.State
	}
	return m
}

// depsSatisfied reports whether every input_from dependency of a step is
// completed.
func depsSatisfied(states map[string]protocol.WorkflowRunStepProgressElemState, inputFrom []string) bool {
	for _, dep := range inputFrom {
		if states[dep] != protocol.WorkflowRunStepProgressElemStateCompleted {
			return false
		}
	}
	return true
}

// NextSteps classifies each not-yet-completed step as ready or blocked (plan
// §5.3). A step is ready only when its input_from dependencies are all
// completed AND its capability is allowed and registered; otherwise it is
// blocked with a human-readable reason. A disallowed/unregistered capability
// therefore surfaces as blocked here, before the agent executes it.
func NextSteps(run protocol.WorkflowRun, steps []StepDef, allowed AllowedFunc) (ready, blocked []StepView) {
	states := stepState(run)
	for _, s := range steps {
		if states[s.ID] == protocol.WorkflowRunStepProgressElemStateCompleted ||
			states[s.ID] == protocol.WorkflowRunStepProgressElemStateSkipped {
			continue
		}
		view := StepView{StepID: s.ID, Capability: s.Capability, Intent: s.Intent, State: string(states[s.ID])}
		switch {
		case allowed != nil && !allowed(s.Capability):
			view.State = string(protocol.WorkflowRunStepProgressElemStateBlocked)
			view.Reason = fmt.Sprintf("capability %q is not allowed by this workflow or is not registered", s.Capability)
			blocked = append(blocked, view)
		case !depsSatisfied(states, s.InputFrom):
			view.State = string(protocol.WorkflowRunStepProgressElemStatePending)
			view.Reason = fmt.Sprintf("waiting on %v", s.InputFrom)
			blocked = append(blocked, view)
		default:
			view.State = string(protocol.WorkflowRunStepProgressElemStateReady)
			ready = append(ready, view)
		}
	}
	return ready, blocked
}

// CompleteStep marks stepID completed with the given evidence and/or deviation
// reason, then recomputes which dependent steps become ready (plan §5.3). It
// rejects: an unknown step, a step whose capability is not allowed/registered
// (rejected before it can progress the run), and completion with neither
// evidence nor a deviation reason. It returns the updated run for the caller
// to persist.
func CompleteStep(run protocol.WorkflowRun, stepID string, evidenceIDs []string, deviationReason string, steps []StepDef, allowed AllowedFunc, now time.Time) (protocol.WorkflowRun, error) {
	var target *StepDef
	for i := range steps {
		if steps[i].ID == stepID {
			target = &steps[i]
			break
		}
	}
	if target == nil {
		return protocol.WorkflowRun{}, fmt.Errorf("workflow: %s: unknown step %q", run.Id, stepID)
	}
	if allowed != nil && !allowed(target.Capability) {
		return protocol.WorkflowRun{}, fmt.Errorf("workflow: %s: step %q capability %q is not allowed by this workflow or is not registered", run.Id, stepID, target.Capability)
	}
	if len(evidenceIDs) == 0 && deviationReason == "" {
		return protocol.WorkflowRun{}, fmt.Errorf("workflow: %s: step %q completion requires evidence ids or a deviation reason", run.Id, stepID)
	}

	found := false
	for i := range run.StepProgress {
		if run.StepProgress[i].StepId != stepID {
			continue
		}
		found = true
		run.StepProgress[i].State = protocol.WorkflowRunStepProgressElemStateCompleted
		if len(evidenceIDs) > 0 {
			run.StepProgress[i].EvidenceIds = evidenceIDs
		}
		if deviationReason != "" {
			d := deviationReason
			run.StepProgress[i].DeviationReason = &d
		}
	}
	if !found {
		return protocol.WorkflowRun{}, fmt.Errorf("workflow: %s: step %q has no progress entry", run.Id, stepID)
	}

	// Recompute readiness: any pending step whose deps are now satisfied and
	// whose capability is allowed becomes ready.
	states := stepState(run)
	for i := range run.StepProgress {
		if run.StepProgress[i].State != protocol.WorkflowRunStepProgressElemStatePending {
			continue
		}
		var def *StepDef
		for j := range steps {
			if steps[j].ID == run.StepProgress[i].StepId {
				def = &steps[j]
				break
			}
		}
		if def == nil {
			continue
		}
		if depsSatisfied(states, def.InputFrom) && (allowed == nil || allowed(def.Capability)) {
			run.StepProgress[i].State = protocol.WorkflowRunStepProgressElemStateReady
		}
	}

	run.UpdatedAt = now
	return run, nil
}

// RecordOutcome attaches a structured outcome to the run (plan §6.1). The
// caller persists the returned run. It does not itself change run state — the
// completion gate (CanComplete) enforces that an outcome exists before a run
// may enter completed.
func RecordOutcome(run protocol.WorkflowRun, outcome protocol.WorkflowRunOutcome, now time.Time) protocol.WorkflowRun {
	if outcome.RecordedAt == nil {
		t := now
		outcome.RecordedAt = &t
	}
	run.Outcome = &outcome
	run.UpdatedAt = now
	return run
}

// IsContextAware reports whether a run participates in the context loop and is
// therefore subject to the outcome completion gate: it either was created from
// a definition or carries a prepared context snapshot.
func IsContextAware(run protocol.WorkflowRun) bool {
	return run.DefinitionRef != nil || run.ContextSnapshot != nil
}

// CanComplete enforces the plan §6.1 completion rules for a run about to enter
// `completed`:
//
//   - a context-aware run must have a recorded outcome;
//   - a definition-backed run must additionally have every step completed or
//     skipped.
//
// Approval and blocking-Bagong checks are enforced separately by the existing
// advance_workflow gate. A run that is neither context-aware nor
// definition-backed is unaffected (backward compatible).
func CanComplete(run protocol.WorkflowRun) error {
	if !IsContextAware(run) {
		return nil
	}
	if run.Outcome == nil {
		return fmt.Errorf("workflow: %s: cannot complete a context-aware run without a recorded outcome (call record_work_outcome first)", run.Id)
	}
	if run.DefinitionRef != nil {
		for _, sp := range run.StepProgress {
			if sp.State != protocol.WorkflowRunStepProgressElemStateCompleted &&
				sp.State != protocol.WorkflowRunStepProgressElemStateSkipped {
				return fmt.Errorf("workflow: %s: cannot complete; step %q is %s (all steps must be completed or skipped)", run.Id, sp.StepId, sp.State)
			}
		}
	}
	return nil
}
