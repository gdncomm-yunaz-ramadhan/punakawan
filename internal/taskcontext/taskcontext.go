// Package taskcontext assembles the fresh, bounded per-task execution
// context described by punakawan-go-typescript-detailed-plan.md §11.2:
// task definition, parent requirement, relevant source excerpts, related
// decisions, affected symbols and files, required tests, previous task
// outputs, and known constraints.
//
// This is deliberately a separate package from internal/dossier.
// internal/dossier assembles Semar's *pre-planning* context-dossier, which
// is a durable protocol.KnowledgeRecord persisted into the knowledge store
// (see protocol/knowledge.schema.json's "context-dossier" record type).
// Context built here is Petruk's *per-task execution* context: §11.2 is
// explicit that each task execution should receive a fresh context and
// that a long conversational history should not be carried across tasks.
// That makes this closer in spirit to an evidence artifact (see §17.2's
// task.yaml) than to durable knowledge, so this package does not define a
// new KnowledgeRecordType and does not touch
// protocol/knowledge.schema.json — Context is a plain Go struct, not a
// protocol.KnowledgeRecord.
//
// §11.2's "task definition" itself is not sourced from here: Beads, not
// the knowledge store, is the system of record for tasks
// (protocol.TaskContract; see internal/tasks). Build has no way to look a
// task up on its own, so the caller must pass the task's own key fields
// (or the whole protocol.TaskContract) into BuildInput.
package taskcontext

import (
	"context"
	"fmt"
	"os"

	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
	"gopkg.in/yaml.v3"
)

// BuildInput carries everything Build needs beyond what it can look up
// itself in the knowledge store.
//
// The task definition (§11.2's first field) is not looked up here: Beads is
// the system of record for tasks, not the knowledge store, so Punakawan's
// own code has no way to derive it. Callers must supply the task's own key
// fields (TaskID, Scope, AcceptanceCriteria, ...) directly, mirroring how
// protocol.TaskContract already carries them (see internal/tasks.go). This
// package accepts the individual fields it needs rather than a
// protocol.TaskContract value so callers that only have a subset of a
// contract (e.g. while building it) are not forced to construct one.
type BuildInput struct {
	// TaskID is this task's stable id (protocol.TaskContract.Id). Required.
	TaskID string
	// RequirementID is the id of the parent requirement knowledge record
	// (protocol.TaskContract.RequirementId) that this task exists to
	// satisfy. Required: Build uses it to look up the parent requirement
	// via store.Get.
	RequirementID string

	// TaskScope, TaskAcceptanceCriteria, TaskDefinitionOfDone, and
	// TaskExpectedFilesOrComponents are copies of the caller's own
	// protocol.TaskContract fields for this task. Build does not validate
	// or look these up; it copies them straight through into Context so
	// that Context.TaskDefinition is self-contained without requiring the
	// caller to hand over a full protocol.TaskContract value.
	TaskScope                     string
	TaskAcceptanceCriteria        []string
	TaskDefinitionOfDone          string
	TaskExpectedFilesOrComponents []string

	// AffectedSymbolsAndFiles names the symbols and/or files this task is
	// expected to touch. Punakawan's own code has no static-analysis pass
	// that derives this from a task id alone, so it is caller-supplied
	// (typically copied from the task's ExpectedFilesOrComponents or a
	// planning step's own analysis).
	AffectedSymbolsAndFiles []string

	// RequiredTests lists the tests this task's work is expected to
	// satisfy or add. Like AffectedSymbolsAndFiles, this has no clean
	// derivation from the knowledge store alone and is caller-supplied
	// (typically copied from the task's TestRequirements).
	RequiredTests []string

	// KnownConstraints lists constraints the caller already knows apply to
	// this task (e.g. non-goals, environment limits) that are not
	// themselves modeled as "constraint" knowledge records. Copied through
	// unchanged; Build also appends any "constraint"-typed knowledge
	// records it finds via store.ListByType, so this field is for
	// constraints that exist only in the caller's head or in the task
	// contract, not yet captured as knowledge.
	KnownConstraints []string

	// PreviousTaskOutputs are short caller-supplied summaries of what
	// earlier tasks in this run produced (e.g. "task bd-a1: added
	// RefundService.Settle"). Per §11.2, later tasks should not receive the
	// full conversational history of earlier tasks — only a bounded
	// summary — so this field intentionally takes short strings rather
	// than, say, full evidence bundles or transcripts. Build also
	// consults store.Related(in.TaskID) for any knowledge records that
	// declare a relation onto this task id (e.g. a "supersedes" or
	// "discovered-from" edge) and appends those, but the primary source is
	// this caller-supplied list, since prior task outputs generally live
	// in Beads/evidence bundles rather than the knowledge store.
	PreviousTaskOutputs []string
}

// Context is the fresh, bounded per-task execution context assembled by
// Build, per §11.2's field list. It is a plain Go struct (marshaled to
// task.yaml by WriteYAML, per §17.2) rather than a protocol.KnowledgeRecord:
// see this package's doc comment for why.
type Context struct {
	// TaskDefinition mirrors the subset of the task's own
	// protocol.TaskContract fields the caller supplied via BuildInput. The
	// caller-owned TaskContract remains the source of truth in Beads; this
	// is a bounded copy for this execution context.
	TaskDefinition TaskDefinition `json:"task_definition" yaml:"task_definition"`

	// ParentRequirement summarizes the requirement knowledge record this
	// task exists to satisfy, looked up from the knowledge store by
	// in.RequirementID.
	ParentRequirement RequirementSummary `json:"parent_requirement" yaml:"parent_requirement"`

	// RelevantSourceExcerpts lists short human-readable references to
	// knowledge records (api-contract and data-contract, mirroring
	// internal/dossier's own choice of "source" material) that may be
	// relevant background for this task's implementation.
	RelevantSourceExcerpts []string `json:"relevant_source_excerpts,omitempty" yaml:"relevant_source_excerpts,omitempty"`

	// RelatedDecisions lists short human-readable references to "decision"
	// knowledge records.
	RelatedDecisions []string `json:"related_decisions,omitempty" yaml:"related_decisions,omitempty"`

	// AffectedSymbolsAndFiles is copied through from BuildInput.
	AffectedSymbolsAndFiles []string `json:"affected_symbols_and_files,omitempty" yaml:"affected_symbols_and_files,omitempty"`

	// RequiredTests is copied through from BuildInput.
	RequiredTests []string `json:"required_tests,omitempty" yaml:"required_tests,omitempty"`

	// PreviousTaskOutputs combines BuildInput's caller-supplied summaries
	// with any knowledge records found to relate onto in.TaskID.
	PreviousTaskOutputs []string `json:"previous_task_outputs,omitempty" yaml:"previous_task_outputs,omitempty"`

	// KnownConstraints combines BuildInput.KnownConstraints with any
	// "constraint"-typed knowledge records found in the store.
	KnownConstraints []string `json:"known_constraints,omitempty" yaml:"known_constraints,omitempty"`
}

// TaskDefinition is the bounded, caller-supplied subset of a
// protocol.TaskContract carried into a Context. See BuildInput's docstring
// for why this package does not look up a task contract on its own.
type TaskDefinition struct {
	TaskID                    string   `json:"task_id" yaml:"task_id"`
	RequirementID             string   `json:"requirement_id" yaml:"requirement_id"`
	Scope                     string   `json:"scope,omitempty" yaml:"scope,omitempty"`
	AcceptanceCriteria        []string `json:"acceptance_criteria,omitempty" yaml:"acceptance_criteria,omitempty"`
	DefinitionOfDone          string   `json:"definition_of_done,omitempty" yaml:"definition_of_done,omitempty"`
	ExpectedFilesOrComponents []string `json:"expected_files_or_components,omitempty" yaml:"expected_files_or_components,omitempty"`
}

// RequirementSummary is a short, bounded summary of the parent requirement
// knowledge record, rather than the full protocol.KnowledgeRecord, to keep
// Context itself bounded per §11.2.
type RequirementSummary struct {
	ID     string `json:"id" yaml:"id"`
	Title  string `json:"title,omitempty" yaml:"title,omitempty"`
	Status string `json:"status,omitempty" yaml:"status,omitempty"`
}

// Build assembles a Context for task in.TaskID against requirement
// in.RequirementID, querying store for the parent requirement record,
// related api/data-contract and decision records, constraint records, and
// any records that relate onto in.TaskID (candidate "previous task output"
// signals).
//
// Build is read-only against store: it never calls store.Put or mutates
// any record.
func Build(ctx context.Context, store *knowledge.Store, in BuildInput) (Context, error) {
	if in.TaskID == "" {
		return Context{}, fmt.Errorf("taskcontext: task id is required")
	}
	if in.RequirementID == "" {
		return Context{}, fmt.Errorf("taskcontext: requirement id is required")
	}

	req, err := store.Get(in.RequirementID)
	if err != nil {
		return Context{}, fmt.Errorf("taskcontext: get parent requirement %q: %w", in.RequirementID, err)
	}
	if req.Type != protocol.KnowledgeRecordTypeRequirement {
		return Context{}, fmt.Errorf("taskcontext: %s: expected a requirement record, got type %q", req.Id, req.Type)
	}

	var relevantSourceExcerpts []string
	for _, t := range []protocol.KnowledgeRecordType{
		protocol.KnowledgeRecordTypeApiContract,
		protocol.KnowledgeRecordTypeDataContract,
	} {
		recs, err := store.ListByType(t)
		if err != nil {
			return Context{}, fmt.Errorf("taskcontext: list knowledge records of type %s: %w", t, err)
		}
		for _, r := range recs {
			relevantSourceExcerpts = append(relevantSourceExcerpts, summarize(r))
		}
	}

	var relatedDecisions []string
	decisions, err := store.ListByType(protocol.KnowledgeRecordTypeDecision)
	if err != nil {
		return Context{}, fmt.Errorf("taskcontext: list decision records: %w", err)
	}
	for _, r := range decisions {
		relatedDecisions = append(relatedDecisions, summarize(r))
	}

	knownConstraints := append([]string{}, in.KnownConstraints...)
	constraints, err := store.ListByType(protocol.KnowledgeRecordTypeConstraint)
	if err != nil {
		return Context{}, fmt.Errorf("taskcontext: list constraint records: %w", err)
	}
	for _, r := range constraints {
		knownConstraints = append(knownConstraints, summarize(r))
	}

	previousTaskOutputs := append([]string{}, in.PreviousTaskOutputs...)
	related, err := store.Related(in.TaskID)
	if err != nil {
		return Context{}, fmt.Errorf("taskcontext: find records related to %q: %w", in.TaskID, err)
	}
	for _, r := range related {
		previousTaskOutputs = append(previousTaskOutputs, summarize(r))
	}

	return Context{
		TaskDefinition: TaskDefinition{
			TaskID:                    in.TaskID,
			RequirementID:             in.RequirementID,
			Scope:                     in.TaskScope,
			AcceptanceCriteria:        in.TaskAcceptanceCriteria,
			DefinitionOfDone:          in.TaskDefinitionOfDone,
			ExpectedFilesOrComponents: in.TaskExpectedFilesOrComponents,
		},
		ParentRequirement: RequirementSummary{
			ID:     req.Id,
			Title:  req.Title,
			Status: req.Status,
		},
		RelevantSourceExcerpts:  relevantSourceExcerpts,
		RelatedDecisions:        relatedDecisions,
		AffectedSymbolsAndFiles: in.AffectedSymbolsAndFiles,
		RequiredTests:           in.RequiredTests,
		PreviousTaskOutputs:     previousTaskOutputs,
		KnownConstraints:        knownConstraints,
	}, nil
}

// WriteYAML marshals c to YAML and writes it to path.
func WriteYAML(c Context, path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("taskcontext: marshal context: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("taskcontext: write %s: %w", path, err)
	}
	return nil
}

// WriteToBundle marshals c to YAML and writes it to bundle's task.yaml
// file, per §17.2's evidence bundle layout (the caller is expected to have
// already created bundle via evidence.NewBundle).
func WriteToBundle(c Context, bundle *evidence.Bundle) error {
	return WriteYAML(c, bundle.Path("task.yaml"))
}

// summarize produces a short human-readable reference to a knowledge
// record, preferring its title and falling back to its id, mirroring
// internal/dossier's summarize helper.
func summarize(rec protocol.KnowledgeRecord) string {
	if rec.Title != "" {
		return rec.Title + " (" + rec.Id + ")"
	}
	return rec.Id
}
