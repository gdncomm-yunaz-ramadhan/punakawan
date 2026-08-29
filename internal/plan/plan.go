// Package plan is the first-class Plan aggregate
// (punakawan-efficiency-project-hygiene-refactor-plan.md §4): a single,
// append-only revisioned unit any delivery or execution worker references
// by an exact plan_id+plan_revision, replacing the three disconnected
// "plan" representations that existed before it (internal/artifact's
// generic markdown PlanStore, protocol.KnowledgeRecordFinalPlan written
// as a plain knowledge record, and delivery's unresolved bare
// PlanRecordID pointer).
//
// Storage lives in the shared SQLite kernel (internal/storage, migration
// 0013_plans.sql): one JSON-encoded row per (id, revision), inserted once
// and never updated. This is deliberately not
// internal/artifact.PlanStore's per-project versions/<n>.md file layout -
// that store is scoped to one project's workspace root, but a Plan can
// name several ProjectIDs and has no single owning workspace to live
// under, and its content is a typed struct rather than opaque markdown.
package plan

import "time"

// PlanStep is one delegable unit of work inside a Plan. Every field is a
// plain string/slice; an empty value means "none" rather than "unset" -
// this type carries no separate presence bit, since §4.3's executable
// check only needs to know whether content exists.
type PlanStep struct {
	// ID identifies this step stably across a plan lineage's revisions, so
	// something that tracks one step's execution (e.g. internal/planexec)
	// can reference it unambiguously even after the plan is revised.
	// Assigned by the caller, or by Store.Save when left empty - see
	// Store.Save for the assignment rule.
	ID              string `json:"id,omitempty"`
	Objective       string `json:"objective"`
	TargetProjectID string `json:"target_project_id,omitempty"`
	TargetRepoID    string `json:"target_repo_id,omitempty"`
	ExpectedOutcome string `json:"expected_outcome"`

	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	VerificationMethod string   `json:"verification_method,omitempty"`

	// DependsOn lists the ID of every PlanStep in the same plan that must
	// be done before this one. A plain blocking list, not a typed
	// dependency taxonomy - nothing in this domain yet needs anything
	// richer than "must finish first".
	DependsOn []string `json:"depends_on,omitempty"`

	// UnresolvedBlockingQuestion, when non-empty, names the one thing
	// standing between this step and delegation to a cheaper
	// implementation worker. Empty means none remain.
	UnresolvedBlockingQuestion string `json:"unresolved_blocking_question,omitempty"`
}

// IsExecutable reports whether step contains everything §4.3 requires
// before it may be delegated to a cheaper implementation worker:
// objective, a target project or repository, expected outcome,
// non-empty acceptance criteria, a verification method, and no
// unresolved blocking question. This is a deterministic completeness
// check, not a fuzzy confidence score - every condition is a plain
// presence test.
func IsExecutable(step PlanStep) bool {
	if step.Objective == "" || step.ExpectedOutcome == "" {
		return false
	}
	if step.TargetProjectID == "" && step.TargetRepoID == "" {
		return false
	}
	if len(step.AcceptanceCriteria) == 0 || step.VerificationMethod == "" {
		return false
	}
	return step.UnresolvedBlockingQuestion == ""
}

// Plan is one revision of a plan lineage. Revision, PreviousRevision, and
// CreatedAt are server-assigned by Store.Save - a caller's supplied
// values for those three are always overwritten, so a Plan a caller
// builds by hand only ever needs to set the content fields plus ID.
type Plan struct {
	ID         string     `json:"id"`
	ProjectIDs []string   `json:"project_ids,omitempty"`
	Revision   int        `json:"revision,omitempty"` // server-assigned by Store.Save; omitempty so plan_save's input schema does not require it
	Objective  string     `json:"objective"`
	Steps      []PlanStep `json:"steps,omitempty"`

	AcceptanceCriteria  []string `json:"acceptance_criteria,omitempty"`
	Verification        string   `json:"verification,omitempty"`
	Assumptions         []string `json:"assumptions,omitempty"`
	UnresolvedQuestions []string `json:"unresolved_questions,omitempty"`

	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"` // server-assigned by Store.Save when zero; omitempty so plan_save's input schema does not require it
	Status    string    `json:"status,omitempty"`

	// PreviousRevision links an append-only revision back to the one it
	// supersedes. Nil on a lineage's first revision.
	PreviousRevision *int   `json:"previous_revision,omitempty"`
	ReasonForChange  string `json:"reason_for_change,omitempty"`

	// The remaining fields are folded in from
	// protocol.KnowledgeRecordFinalPlan (§4.4): richer optional content
	// that proved useful there and is kept here rather than dropped, now
	// living on the one first-class Plan aggregate instead of a second
	// parallel shape.
	Requirements                []string          `json:"requirements,omitempty"`
	NonGoals                    []string          `json:"non_goals,omitempty"`
	ArchitectureDecision        string            `json:"architecture_decision,omitempty"`
	DataModelImpact             string            `json:"data_model_impact,omitempty"`
	ApiImpact                   string            `json:"api_impact,omitempty"`
	RepositoryImpactMap         map[string]string `json:"repository_impact_map,omitempty"`
	ImplementationSequence      []string          `json:"implementation_sequence,omitempty"`
	UnitTestPlan                []string          `json:"unit_test_plan,omitempty"`
	IntegrationTestPlan         []string          `json:"integration_test_plan,omitempty"`
	E2EPlan                     []string          `json:"e2e_plan,omitempty"`
	MigrationPlan               []string          `json:"migration_plan,omitempty"`
	RollbackPlan                []string          `json:"rollback_plan,omitempty"`
	ObservabilityPlan           []string          `json:"observability_plan,omitempty"`
	DocumentationPlan           []string          `json:"documentation_plan,omitempty"`
	DeploymentChanges           []string          `json:"deployment_changes,omitempty"`
	SecurityConsiderations      []string          `json:"security_considerations,omitempty"`
	CompatibilityConsiderations []string          `json:"compatibility_considerations,omitempty"`
	VerificationCriteria        []string          `json:"verification_criteria,omitempty"`
	RisksAndMitigations         []string          `json:"risks_and_mitigations,omitempty"`
}
