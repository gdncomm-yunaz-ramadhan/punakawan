package handoff

import (
	"github.com/ygrip/punakawan/pkg/protocol"
)

// This file implements the §41 capsule-assembly API (HANDOFF-007..010). Rather
// than making a caller hand-populate a protocol.HandoffCapsule, each panel role
// contributes only the section it owns:
//
//   - Semar   the objective, current phase, accepted-plan reference, and the
//     role-configuration revision the work was accepted under (coordination);
//   - Gareng  the open contradictions, unresolved risks, and impact summary;
//   - Petruk  the changed repositories, completed tasks, and current task;
//   - Bagong  the evidence references and dossier verification status.
//
// The contributions are plain value structs with role-neutral field names, so
// this package stays dependency-free (it imports only pkg/protocol, the shared
// wire types, never the plan/roleconfig/contradiction/dossier subsystems). The
// builder maps them onto the exact protocol.HandoffCapsule shape and stamps the
// schema version and id so the result is a valid capsule ready for Create.

// Objective is Semar's statement of what the run is trying to achieve, plus the
// external references (jira/etc.) that ground it. Mirrors the capsule's
// objective block.
type Objective struct {
	Statement  string
	SourceRefs []string
}

// PlanRef references an accepted plan by id and version. It is a reference, not
// a copy: the plan body lives in the plan store.
type PlanRef struct {
	ID      string
	Version int
}

// TaskRef references the current task by id and records the concrete next
// action a resumer should take.
type TaskRef struct {
	ID         string
	NextAction string
}

// DossierRef references the change dossier by id and records its verification
// status at handoff time.
type DossierRef struct {
	ID     string
	Status string
}

// ImpactSummary is Gareng's required/excluded repository partition for the run.
type ImpactSummary struct {
	RequiredRepositories []string
	ExcludedRepositories []string
}

// SemarContribution is the coordination section: the objective, the current
// phase, the accepted plan, and the role-configuration revision the work was
// accepted under. It is the only required contribution - a capsule cannot be
// assembled without it because it carries the schema-required objective and
// current_phase.
type SemarContribution struct {
	Objective          Objective
	CurrentPhase       string
	AcceptedPlan       *PlanRef
	RoleConfigRevision int
}

// GarengContribution is the contradiction/risk section: what is still open, the
// risks that remain unresolved, and the impact summary Gareng computed.
type GarengContribution struct {
	OpenContradictions []string
	UnresolvedRisks    []string
	ImpactSummary      *ImpactSummary
}

// PetrukContribution is the implementation section: which repositories changed,
// which tasks are done, and which task is in flight with its next action.
type PetrukContribution struct {
	ChangedRepositories []string
	CompletedTasks      []string
	CurrentTask         *TaskRef
}

// BagongContribution is the verification section: the evidence references
// gathered and the dossier's verification status.
type BagongContribution struct {
	Evidence []string
	Dossier  *DossierRef
}

// CapsuleBuilder accumulates per-role contributions onto a base capsule seeded
// by NewCapsule. It is not safe for concurrent use; assemble a capsule from a
// single goroutine. Zero-value contributions (nil pointers, empty slices) leave
// the corresponding capsule fields unset, so the capsule stays as small as the
// contributions allow.
type CapsuleBuilder struct {
	capsule protocol.HandoffCapsule
}

// NewCapsule starts assembling a capsule for the given project and run from
// Semar's required coordination section. The returned builder already carries a
// valid, Create-ready capsule (schema version, a derived id, project/run ids,
// objective, and current phase); WithGareng/WithPetruk/WithBagong layer on the
// optional sections. The id defaults to "handoff-<runID>"; a caller that needs
// several capsules per run can override it via WithID before Build.
func NewCapsule(projectID, runID string, semar SemarContribution) *CapsuleBuilder {
	role := "semar"
	h := protocol.HandoffCapsule{
		Version:      SupportedVersion,
		Id:           "handoff-" + runID,
		ProjectId:    projectID,
		RunId:        runID,
		CurrentPhase: semar.CurrentPhase,
		Objective: protocol.HandoffCapsuleObjective{
			Statement:  semar.Objective.Statement,
			SourceRefs: semar.Objective.SourceRefs,
		},
		RoleConfigurationRevision: intPtr(semar.RoleConfigRevision),
		CreatedBy:                 &protocol.HandoffCapsuleCreatedBy{Role: &role},
	}
	if semar.AcceptedPlan != nil {
		h.AcceptedPlan = &protocol.HandoffCapsuleAcceptedPlan{
			Id:      strPtr(semar.AcceptedPlan.ID),
			Version: intPtr(semar.AcceptedPlan.Version),
		}
	}
	return &CapsuleBuilder{capsule: h}
}

// WithID overrides the derived capsule id (e.g. to disambiguate several
// handoffs within one run, as in the §41 example "handoff-run-1842-03").
func (b *CapsuleBuilder) WithID(id string) *CapsuleBuilder {
	b.capsule.Id = id
	return b
}

// WithGareng adds Gareng's contradiction/risk/impact section.
func (b *CapsuleBuilder) WithGareng(g GarengContribution) *CapsuleBuilder {
	b.capsule.OpenContradictions = g.OpenContradictions
	b.capsule.UnresolvedRisks = g.UnresolvedRisks
	if g.ImpactSummary != nil {
		b.capsule.ImpactSummary = &protocol.HandoffCapsuleImpactSummary{
			RequiredRepositories: g.ImpactSummary.RequiredRepositories,
			ExcludedRepositories: g.ImpactSummary.ExcludedRepositories,
		}
	}
	return b
}

// WithPetruk adds Petruk's implementation section.
func (b *CapsuleBuilder) WithPetruk(p PetrukContribution) *CapsuleBuilder {
	b.capsule.ChangedRepositories = p.ChangedRepositories
	b.capsule.CompletedTasks = p.CompletedTasks
	if p.CurrentTask != nil {
		b.capsule.CurrentTask = &protocol.HandoffCapsuleCurrentTask{
			Id:         strPtr(p.CurrentTask.ID),
			NextAction: strPtr(p.CurrentTask.NextAction),
		}
	}
	return b
}

// WithBagong adds Bagong's evidence/dossier verification section.
func (b *CapsuleBuilder) WithBagong(bg BagongContribution) *CapsuleBuilder {
	b.capsule.Evidence = bg.Evidence
	if bg.Dossier != nil {
		b.capsule.Dossier = &protocol.HandoffCapsuleDossier{
			Id:     strPtr(bg.Dossier.ID),
			Status: strPtr(bg.Dossier.Status),
		}
	}
	return b
}

// Build returns the assembled capsule, ready to pass to Create.
func (b *CapsuleBuilder) Build() protocol.HandoffCapsule {
	return b.capsule
}

// Assemble is the one-shot form of the builder for callers (such as the
// create_handoff_capsule MCP tool) that have all contributions in hand: Semar
// is required, the other three are optional and skipped when nil. It is exactly
// equivalent to NewCapsule followed by the matching With* calls and Build.
func Assemble(projectID, runID string, semar SemarContribution, gareng *GarengContribution, petruk *PetrukContribution, bagong *BagongContribution) protocol.HandoffCapsule {
	b := NewCapsule(projectID, runID, semar)
	if gareng != nil {
		b.WithGareng(*gareng)
	}
	if petruk != nil {
		b.WithPetruk(*petruk)
	}
	if bagong != nil {
		b.WithBagong(*bagong)
	}
	return b.Build()
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
