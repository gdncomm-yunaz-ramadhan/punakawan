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
	return createTaskForRequirement(ctx, sup, dir, store, req, in)
}

// ReportDiscoveredWork records newly discovered work found mid-execution of
// discoveredFromTaskID, per §10.4's discovery rule: "Petruk must not
// silently increase scope. Newly discovered work becomes a discovered-from
// task and is reviewed by Semar."
//
// It does exactly what CreateTaskForRequirement does (same requirement-type
// check, TaskContract construction, Beads issue creation, and tracked-by
// relation persistence via createTaskForRequirement), plus two additions
// that make the discovery rule hold:
//
//  1. in.DiscoveredFrom is forced to point at discoveredFromTaskID. If the
//     caller already set in.DiscoveredFrom to a different, non-empty value,
//     that is treated as a conflicting instruction and rejected with an
//     error rather than silently overwritten — the caller's disagreeing
//     value is more likely a bug on the caller's side than something this
//     function should paper over.
//  2. The created Beads issue's labels always include "discovered" and
//     "needs-semar-review" in addition to whatever in.BeadsLabels the
//     caller supplied. Semar (an external MCP-client role per the existing
//     M3 architecture) can then find every discovered task via
//     `bd list --label needs-semar-review` without this package needing to
//     build a dedicated review-queue subsystem.
func ReportDiscoveredWork(ctx context.Context, sup *tools.Supervisor, dir string, store *knowledge.Store, req protocol.KnowledgeRecord, discoveredFromTaskID string, in NewTaskContractInput) (protocol.TaskContract, error) {
	if discoveredFromTaskID == "" {
		return protocol.TaskContract{}, fmt.Errorf("tasks: discovered-from task id is required")
	}
	if in.DiscoveredFrom != nil && *in.DiscoveredFrom != discoveredFromTaskID {
		return protocol.TaskContract{}, fmt.Errorf("tasks: conflicting discovered_from: input specified %q, but discoveredFromTaskID is %q", *in.DiscoveredFrom, discoveredFromTaskID)
	}
	in.DiscoveredFrom = &discoveredFromTaskID
	in.BeadsLabels = appendMissingLabels(in.BeadsLabels, "discovered", "needs-semar-review")

	return createTaskForRequirement(ctx, sup, dir, store, req, in)
}

// appendMissingLabels returns labels with each of extra appended, skipping
// any that are already present, so repeated calls (or a caller that already
// supplied one of these labels) do not produce duplicate `bd create
// --labels` entries.
func appendMissingLabels(labels []string, extra ...string) []string {
	for _, e := range extra {
		found := false
		for _, l := range labels {
			if l == e {
				found = true
				break
			}
		}
		if !found {
			labels = append(labels, e)
		}
	}
	return labels
}

// createTaskForRequirement is the shared implementation behind
// CreateTaskForRequirement and ReportDiscoveredWork: it builds the
// protocol.TaskContract, creates the Beads issue, and persists the
// tracked-by relation. See CreateTaskForRequirement's doc comment for the
// full behavior; ReportDiscoveredWork's callers reach this after adjusting
// in.DiscoveredFrom and in.BeadsLabels.
func createTaskForRequirement(ctx context.Context, sup *tools.Supervisor, dir string, store *knowledge.Store, req protocol.KnowledgeRecord, in NewTaskContractInput) (protocol.TaskContract, error) {
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

// GraphItem is one task to create as part of a batch GenerateGraph call.
// LocalKey identifies the item only within that call (it is never
// persisted), so DependsOn can reference sibling items before any of them
// have a real Beads ID.
type GraphItem struct {
	LocalKey      string
	RequirementID string
	Input         NewTaskContractInput
	DependsOn     []GraphDependency
}

// GraphDependency is a dependency edge from one GraphItem onto another
// item in the same GenerateGraph call, referenced by LocalKey.
type GraphDependency struct {
	LocalKey string
	Type     protocol.TaskContractDependenciesElemType
}

// GraphResult pairs a created TaskContract with the LocalKey it was built
// from.
type GraphResult struct {
	LocalKey string
	Contract protocol.TaskContract
}

// GenerateGraph batch-creates a set of TaskContracts and wires the
// dependency edges between them, per §10.1-§10.4. Punakawan never calls an
// LLM itself (§28): the calling role (Petruk/Semar) does the actual
// decomposition; this function only creates and wires the resulting graph
// deterministically, in two passes:
//
//  1. Every LocalKey and DependsOn reference is validated up front (unique
//     keys, no reference to an unknown key) before anything is created, so
//     a malformed graph fails with zero side effects rather than partially
//     creating Beads issues.
//  2. Each item's Beads issue and TaskContract are created via the same
//     createTaskForRequirement path CreateTaskForRequirement uses. Once
//     every item has a Beads ID, WireDependency is called for each
//     DependsOn edge, translating LocalKeys to real Beads IDs.
//
// If an item's Input.Dependencies is left empty, it is populated from
// DependsOn (using each target's own Input.TaskID as the contract-level
// reference) so callers do not have to state the same edges twice; an
// explicitly supplied Input.Dependencies is left untouched.
func GenerateGraph(ctx context.Context, sup *tools.Supervisor, dir string, store *knowledge.Store, items []GraphItem) ([]GraphResult, error) {
	byKey := make(map[string]*GraphItem, len(items))
	for i := range items {
		item := &items[i]
		if item.LocalKey == "" {
			return nil, fmt.Errorf("tasks: item %d: local_key is required", i)
		}
		if _, dup := byKey[item.LocalKey]; dup {
			return nil, fmt.Errorf("tasks: duplicate local_key %q", item.LocalKey)
		}
		byKey[item.LocalKey] = item
	}
	for _, item := range items {
		for _, dep := range item.DependsOn {
			if _, ok := byKey[dep.LocalKey]; !ok {
				return nil, fmt.Errorf("tasks: item %q: depends_on unknown local_key %q", item.LocalKey, dep.LocalKey)
			}
		}
	}

	for i := range items {
		item := &items[i]
		if len(item.Input.Dependencies) > 0 {
			continue
		}
		for _, dep := range item.DependsOn {
			item.Input.Dependencies = append(item.Input.Dependencies, protocol.TaskContractDependenciesElem{
				Type: dep.Type,
				Id:   byKey[dep.LocalKey].Input.TaskID,
			})
		}
	}

	results := make([]GraphResult, len(items))
	resultByKey := make(map[string]GraphResult, len(items))
	for i, item := range items {
		req, err := store.Get(item.RequirementID)
		if err != nil {
			return nil, fmt.Errorf("tasks: item %q: load requirement %q: %w", item.LocalKey, item.RequirementID, err)
		}
		contract, err := createTaskForRequirement(ctx, sup, dir, store, req, item.Input)
		if err != nil {
			return nil, fmt.Errorf("tasks: item %q: %w", item.LocalKey, err)
		}
		results[i] = GraphResult{LocalKey: item.LocalKey, Contract: contract}
		resultByKey[item.LocalKey] = results[i]
	}

	for _, item := range items {
		from := resultByKey[item.LocalKey]
		if from.Contract.BeadsEpic == nil {
			continue
		}
		for _, dep := range item.DependsOn {
			to := resultByKey[dep.LocalKey]
			if to.Contract.BeadsEpic == nil {
				continue
			}
			if err := WireDependency(ctx, sup, dir, *from.Contract.BeadsEpic, *to.Contract.BeadsEpic, dep.Type); err != nil {
				return nil, fmt.Errorf("tasks: wire dependency %q -> %q: %w", item.LocalKey, dep.LocalKey, err)
			}
		}
	}

	return results, nil
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
