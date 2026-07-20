// Package tasks implements requirement-to-task mapping, per
// punakawan-go-typescript-detailed-plan.md §10 ("Beads Task Generation"):
// Jira remains the human-facing tracker, and Beads becomes the detailed
// local execution graph. §10.1 defines the identifier mapping
// (requirement_id / jira_key / beads_epic), and §10.3 defines the task
// contract fields.
//
// This lives in its own package, separate from internal/beads and
// internal/knowledge, because it composes both: it turns a requirement
// protocol.KnowledgeRecord plus caller-supplied scope/repository/acceptance
// data into a protocol.TaskContract, creates the corresponding Beads issue
// via internal/beads, and records the resulting link back into the
// knowledge store via internal/knowledge's Store.Put. Neither of those two
// packages should depend on the other's concept (beads.go has no notion of
// requirements; knowledge's Store has no notion of Beads), so the mapping
// itself needs a home that depends on both. internal/tasks already existed
// as an empty placeholder package in this repository with no prior content
// or plan cross-reference, making it the natural (rather than newly
// invented) home for this.
package tasks

import (
	"context"
	"fmt"

	"github.com/ygrip/punakawan/internal/beads"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// jiraProvider is the literal source.provider value that marks a
// requirement's source as Jira, per punakawan-go-typescript-detailed-plan.md
// §7.3's example record (source.provider: jira, source.external_id:
// PAY-1842).
const jiraProvider = "jira"

// NewTaskContractInput holds everything needed to build a
// protocol.TaskContract for a requirement, beyond what the requirement
// KnowledgeRecord itself carries. These fields are not derivable from the
// requirement record alone (§10.3 lists them as independent parts of the
// task contract), so the caller (e.g. Petruk's planning step, §11) must
// supply them.
type NewTaskContractInput struct {
	// TaskID is the stable task ID (§10.3 "Stable task ID"). This is
	// assigned by the caller before the Beads issue exists (e.g. derived
	// from the plan), not generated here.
	TaskID string
	// Repository is the affected repository (§10.3).
	Repository string
	// Dependencies are this task's dependency edges (§10.3), e.g. a
	// "blocks" edge onto another TaskContract's ID. This function does not
	// wire these into Beads (see WireDependency for that); it only carries
	// them into the contract.
	Dependencies []protocol.TaskContractDependenciesElem
	// Scope describes the task's scope (§10.3).
	Scope string
	// ExpectedFilesOrComponents lists expected files or components (§10.3).
	ExpectedFilesOrComponents []string
	// AcceptanceCriteria is required by the schema (minItems: 1).
	AcceptanceCriteria []string
	// TestRequirements lists test requirements (§10.3).
	TestRequirements []string
	// RequiredEvidence lists required evidence (§10.3).
	RequiredEvidence []string
	// RiskClassification is the task's risk classification (§10.3).
	RiskClassification protocol.TaskContractRiskClassification
	// ApprovalRequired indicates whether approval is required (§10.3).
	ApprovalRequired *bool
	// DefinitionOfDone is required by the schema.
	DefinitionOfDone string
	// DiscoveredFrom names the task this one was discovered from, if any
	// (§10.4 discovery rule).
	DiscoveredFrom *string

	// BeadsParent is the Beads issue ID to create this task's issue as a
	// hierarchical child of (typically the requirement's Beads epic). Empty
	// creates a top-level Beads issue.
	BeadsParent string
	// BeadsType is the Beads issue type (e.g. "task", "epic"). Empty defers
	// to bd's own default.
	BeadsType string
	// BeadsLabels are applied to the created Beads issue.
	BeadsLabels []string
}

// CreateTaskForRequirement builds a protocol.TaskContract for req (which
// must have Type == KnowledgeRecordTypeRequirement), creates the
// corresponding Beads issue for it, and persists a "tracked-by" relation
// (§7.2) from req to the new Beads issue id via store.Put.
//
// Per §10.1's mapping:
//   - requirement_id is set from req.Id.
//   - jira_key is set from req.Source.ExternalId, but only when
//     req.Source.Provider == "jira" (per §7.3's example record); otherwise
//     it is left empty, since a non-Jira source has no Jira key to record.
//   - beads_epic is set to the Beads issue id returned by creating the
//     task, i.e. the newly created task's own tracking issue. (§10.1's
//     example shows beads_epic as a property of the requirement mapping
//     alongside jira_key; at the level of an individual TaskContract this
//     is the Beads id that tracks that specific task.)
//
// sup and dir are the Supervisor and working directory used to invoke the
// bd CLI (dir must contain an initialized bd project); store is the
// knowledge Store the tracked-by relation is persisted into.
func CreateTaskForRequirement(ctx context.Context, sup *tools.Supervisor, dir string, store *knowledge.Store, req protocol.KnowledgeRecord, in NewTaskContractInput) (protocol.TaskContract, error) {
	if req.Type != protocol.KnowledgeRecordTypeRequirement {
		return protocol.TaskContract{}, fmt.Errorf("tasks: %s: expected a requirement record, got type %q", req.Id, req.Type)
	}
	if in.TaskID == "" {
		return protocol.TaskContract{}, fmt.Errorf("tasks: task id is required")
	}
	if len(in.AcceptanceCriteria) == 0 {
		return protocol.TaskContract{}, fmt.Errorf("tasks: %s: acceptance_criteria must have at least one entry", in.TaskID)
	}

	contract := protocol.TaskContract{
		Id:                        in.TaskID,
		RequirementId:             req.Id,
		Repository:                in.Repository,
		Dependencies:              in.Dependencies,
		Scope:                     in.Scope,
		ExpectedFilesOrComponents: in.ExpectedFilesOrComponents,
		AcceptanceCriteria:        in.AcceptanceCriteria,
		TestRequirements:          in.TestRequirements,
		RequiredEvidence:          in.RequiredEvidence,
		ApprovalRequired:          in.ApprovalRequired,
		DefinitionOfDone:          in.DefinitionOfDone,
		DiscoveredFrom:            in.DiscoveredFrom,
	}
	if in.RiskClassification != "" {
		rc := in.RiskClassification
		contract.RiskClassification = &rc
	}
	if req.Source.Provider == jiraProvider && req.Source.ExternalId != nil && *req.Source.ExternalId != "" {
		jiraKey := *req.Source.ExternalId
		contract.JiraKey = &jiraKey
	}

	description := in.Scope
	beadsID, err := beads.CreateTask(ctx, sup, dir, req.Title, description, beads.CreateTaskOptions{
		Type:               in.BeadsType,
		Parent:             in.BeadsParent,
		Labels:             in.BeadsLabels,
		AcceptanceCriteria: in.AcceptanceCriteria,
	})
	if err != nil {
		return protocol.TaskContract{}, fmt.Errorf("tasks: create beads issue for %s: %w", in.TaskID, err)
	}
	contract.BeadsEpic = &beadsID

	req.Relations = append(req.Relations, protocol.KnowledgeRecordRelationsElem{
		Type:   protocol.KnowledgeRecordRelationsElemTypeTrackedBy,
		Target: beadsID,
	})
	if err := store.Put(req); err != nil {
		return protocol.TaskContract{}, fmt.Errorf("tasks: persist tracked-by relation for %s -> %s: %w", req.Id, beadsID, err)
	}

	return contract, nil
}

// WireDependency creates a Beads dependency edge matching a
// protocol.TaskContractDependenciesElem between two already-created Beads
// issues, translating the task contract's dependency vocabulary (blocks |
// discovered-from | requires, per protocol/task.schema.json) into bd's own
// `bd dep add --type` vocabulary.
//
// fromBeadsID and toBeadsID are Beads issue ids (e.g. the BeadsEpic values
// from two TaskContracts), not TaskContract or requirement ids: Beads has
// no notion of the latter.
func WireDependency(ctx context.Context, sup *tools.Supervisor, dir, fromBeadsID, toBeadsID string, depType protocol.TaskContractDependenciesElemType) error {
	bdType, err := beadsDependencyType(depType)
	if err != nil {
		return err
	}
	return beads.AddDependency(ctx, sup, dir, fromBeadsID, toBeadsID, bdType)
}

// beadsDependencyType maps protocol/task.schema.json's dependency type enum
// (blocks | discovered-from | requires) onto bd dep add --type's enum
// (verified via `bd dep add --help`: blocks|tracks|related|parent-child|
// discovered-from|until|caused-by|validates|relates-to|supersedes).
//
// "blocks" and "discovered-from" are literal, identically-named members of
// both enums. The schema's "requires" has no identically-named counterpart
// in bd's enum; bd's own dependency-direction convention (`bd dep add
// blocked blocker`, i.e. the first issue depends on / is blocked by the
// second) means a task contract's "requires" dependency — this task
// requires another to exist first — is exactly a "blocks" edge from bd's
// point of view, just named from the other task's side. This equivalence
// (not a literal string match) is this package's judgment call, since §10.3
// does not define bd-side semantics.
func beadsDependencyType(depType protocol.TaskContractDependenciesElemType) (string, error) {
	switch depType {
	case protocol.TaskContractDependenciesElemTypeBlocks:
		return "blocks", nil
	case protocol.TaskContractDependenciesElemTypeDiscoveredFrom:
		return "discovered-from", nil
	case protocol.TaskContractDependenciesElemTypeRequires:
		return "blocks", nil
	default:
		return "", fmt.Errorf("tasks: unknown dependency type %q", depType)
	}
}
