package plan

import (
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// FromFinalPlanRecord is §4.4's compatibility read path: a best-effort,
// Plan-shaped view of a historical protocol.KnowledgeRecordFinalPlan,
// for any caller still holding an old
// delivery.OrchestrationDetails.PlanRecordID. It is intentionally lossy
// - the old record has no objective, steps, project_ids, or revision
// lineage, so those are synthesized (Objective from the record's title)
// or left empty rather than invented. Nothing new is ever written this
// way; see Store.Save for the only write path.
func FromFinalPlanRecord(rec protocol.KnowledgeRecord) (Plan, error) {
	if rec.FinalPlan == nil {
		return Plan{}, fmt.Errorf("plan: knowledge record %s has no final_plan body", rec.Id)
	}
	fp := *rec.FinalPlan

	p := Plan{
		ID:                          rec.Id,
		Revision:                    1,
		Objective:                   rec.Title,
		CreatedBy:                   rec.Source.Provider,
		CreatedAt:                   rec.Source.RetrievedAt,
		Status:                      "legacy-knowledge",
		Requirements:                fp.Requirements,
		AcceptanceCriteria:          fp.AcceptanceCriteria,
		NonGoals:                    fp.NonGoals,
		ImplementationSequence:      fp.ImplementationSequence,
		UnitTestPlan:                fp.UnitTestPlan,
		IntegrationTestPlan:         fp.IntegrationTestPlan,
		E2EPlan:                     fp.E2EPlan,
		MigrationPlan:               fp.MigrationPlan,
		RollbackPlan:                fp.RollbackPlan,
		ObservabilityPlan:           fp.ObservabilityPlan,
		DocumentationPlan:           fp.DocumentationPlan,
		DeploymentChanges:           fp.DeploymentChanges,
		SecurityConsiderations:      fp.SecurityConsiderations,
		CompatibilityConsiderations: fp.CompatibilityConsiderations,
		VerificationCriteria:        fp.VerificationCriteria,
		RisksAndMitigations:         fp.RisksAndMitigations,
		RepositoryImpactMap:         map[string]string(fp.RepositoryImpactMap),
	}
	if fp.ApiImpact != nil {
		p.ApiImpact = *fp.ApiImpact
	}
	if fp.ArchitectureDecision != nil {
		p.ArchitectureDecision = *fp.ArchitectureDecision
	}
	if fp.DataModelImpact != nil {
		p.DataModelImpact = *fp.DataModelImpact
	}
	if len(fp.VerificationCriteria) > 0 {
		p.Verification = strings.Join(fp.VerificationCriteria, "\n")
	}
	return p, nil
}

// FromFinalPlanInput builds a new Plan from submit_final_plan's existing
// input shape (protocol.KnowledgeRecordFinalPlan), so that tool's
// long-standing callers keep working unchanged while the plan they
// submit lands in the Plan domain rather than as a knowledge record
// (§4.4). id and title come from the MCP handler (the same recordID/
// title the tool always took); the two length checks mirror
// roles.SubmitFinalPlan's existing validation exactly, so a submission
// that used to be rejected still is.
func FromFinalPlanInput(id, title string, fp protocol.KnowledgeRecordFinalPlan) (Plan, error) {
	if len(fp.Requirements) == 0 {
		return Plan{}, fmt.Errorf("plan: final plan %s: requirements must have at least one entry", id)
	}
	if len(fp.AcceptanceCriteria) == 0 {
		return Plan{}, fmt.Errorf("plan: final plan %s: acceptance_criteria must have at least one entry", id)
	}

	p := Plan{
		ID:                          id,
		Objective:                   title,
		Status:                      "final",
		Requirements:                fp.Requirements,
		AcceptanceCriteria:          fp.AcceptanceCriteria,
		NonGoals:                    fp.NonGoals,
		ImplementationSequence:      fp.ImplementationSequence,
		UnitTestPlan:                fp.UnitTestPlan,
		IntegrationTestPlan:         fp.IntegrationTestPlan,
		E2EPlan:                     fp.E2EPlan,
		MigrationPlan:               fp.MigrationPlan,
		RollbackPlan:                fp.RollbackPlan,
		ObservabilityPlan:           fp.ObservabilityPlan,
		DocumentationPlan:           fp.DocumentationPlan,
		DeploymentChanges:           fp.DeploymentChanges,
		SecurityConsiderations:      fp.SecurityConsiderations,
		CompatibilityConsiderations: fp.CompatibilityConsiderations,
		VerificationCriteria:        fp.VerificationCriteria,
		RisksAndMitigations:         fp.RisksAndMitigations,
		RepositoryImpactMap:         map[string]string(fp.RepositoryImpactMap),
	}
	if fp.ApiImpact != nil {
		p.ApiImpact = *fp.ApiImpact
	}
	if fp.ArchitectureDecision != nil {
		p.ArchitectureDecision = *fp.ArchitectureDecision
	}
	if fp.DataModelImpact != nil {
		p.DataModelImpact = *fp.DataModelImpact
	}
	if len(fp.VerificationCriteria) > 0 {
		p.Verification = strings.Join(fp.VerificationCriteria, "\n")
	}
	return p, nil
}
