package plan

import (
	"github.com/ygrip/punakawan/internal/workflowdef"
)

// FromWorkflowDefinition builds a fresh Plan lineage (a new NewID, not a
// revision of an existing one) from a workflowdef.Definition, for §5.3's
// workflow -> plan -> delivery pipeline: invoking a definition
// instantiates a Plan before creating its run/delivery, so the result
// always references an exact plan_id+plan_revision.
//
// The mapping is necessarily lossy: workflowdef.Step carries only
// Capability/Intent/ID/InputFrom, none of which name a target
// project/repo, acceptance criteria, or a verification method, so those
// PlanStep fields are left empty rather than invented. IsExecutable
// returning false for a step built this way is expected until a human
// fills in the missing content - not a bug to work around.
func FromWorkflowDefinition(def workflowdef.Definition) Plan {
	objective := def.Name
	if objective == "" {
		objective = def.ID
	}

	steps := make([]PlanStep, 0, len(def.Steps))
	for _, step := range def.Steps {
		steps = append(steps, PlanStep{Objective: step.Intent})
	}

	return Plan{
		ID:        NewID(),
		Objective: objective,
		Steps:     steps,
		Status:    "workflow-definition",
	}
}
