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
	"errors"
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

	// Previous is the Context this same TaskID's last build_task_context
	// call produced (loaded from its task.yaml evidence, if one exists),
	// used to fill in whichever of TaskScope, TaskAcceptanceCriteria,
	// TaskDefinitionOfDone, TaskExpectedFilesOrComponents,
	// AffectedSymbolsAndFiles, and RequiredTests the caller leaves at their
	// zero value on a resume call. This is punokawan-d87's fix: a task
	// commonly resumes across more than one build_task_context/
	// start_task_execution round (impl -> tests -> review), and without
	// this, each round had to resend the full scope/AC/constraints text
	// verbatim even when only one field actually changed. RelatedDecisions,
	// RelevantSourceExcerpts, KnownConstraints, and PreviousTaskOutputs are
	// deliberately NOT inherited from Previous - Build already re-derives
	// those fresh from the live knowledge store every call, which is
	// strictly more current than replaying Previous's already-merged copy
	// (and, for KnownConstraints/PreviousTaskOutputs specifically, would
	// double-append the store-derived portion on every resume). nil is the
	// normal value for a task's first build_task_context call.
	Previous *Context

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

	in = applyPrevious(in)

	req, err := store.Get(in.RequirementID)
	if errors.Is(err, knowledge.ErrNotFound) {
		return Context{}, fmt.Errorf("taskcontext: no requirement record %q exists yet; for a Jira-sourced requirement, call ingest_jira_requirement first: %w", in.RequirementID, err)
	}
	if err != nil {
		return Context{}, fmt.Errorf("taskcontext: get parent requirement %q: %w", in.RequirementID, err)
	}
	if req.Type != protocol.KnowledgeRecordTypeRequirement {
		return Context{}, fmt.Errorf("taskcontext: %s: expected a requirement record, got type %q", req.Id, req.Type)
	}

	// The api/data-contract, decision, and constraint lists are named
	// "relevant"/"related"/"known" but were previously every record of each
	// type in the whole store, unbounded - straight model-context bloat as the
	// store grows (punokawan-d9v). Filter each to records whose own scope does
	// not conflict with the parent requirement's scope, and cap each list, so
	// the naming is honest and the context stays bounded.
	reqScope := req.Scope

	var relevantSourceExcerpts []string
	for _, t := range []protocol.KnowledgeRecordType{
		protocol.KnowledgeRecordTypeApiContract,
		protocol.KnowledgeRecordTypeDataContract,
	} {
		recs, err := store.ListByType(t)
		if err != nil {
			return Context{}, fmt.Errorf("taskcontext: list knowledge records of type %s: %w", t, err)
		}
		relevantSourceExcerpts = appendScopedSummaries(relevantSourceExcerpts, recs, reqScope, maxRelevantSourceExcerpts)
	}

	var relatedDecisions []string
	decisions, err := store.ListByType(protocol.KnowledgeRecordTypeDecision)
	if err != nil {
		return Context{}, fmt.Errorf("taskcontext: list decision records: %w", err)
	}
	relatedDecisions = appendScopedSummaries(relatedDecisions, decisions, reqScope, maxRelatedDecisions)

	// Caller-supplied constraints are always kept (they came with the task);
	// only the store-derived additions are scope-filtered and capped.
	knownConstraints := append([]string{}, in.KnownConstraints...)
	constraints, err := store.ListByType(protocol.KnowledgeRecordTypeConstraint)
	if err != nil {
		return Context{}, fmt.Errorf("taskcontext: list constraint records: %w", err)
	}
	knownConstraints = appendScopedSummaries(knownConstraints, constraints, reqScope, len(knownConstraints)+maxKnownConstraints)

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

// applyPrevious fills in whichever of in's inheritable fields are at their
// zero value from in.Previous, per BuildInput.Previous's doc comment. It
// leaves in unchanged when Previous is nil (a task's first
// build_task_context call) or when a field was already supplied.
func applyPrevious(in BuildInput) BuildInput {
	if in.Previous == nil {
		return in
	}
	prev := in.Previous.TaskDefinition
	if in.TaskScope == "" {
		in.TaskScope = prev.Scope
	}
	if len(in.TaskAcceptanceCriteria) == 0 {
		in.TaskAcceptanceCriteria = prev.AcceptanceCriteria
	}
	if in.TaskDefinitionOfDone == "" {
		in.TaskDefinitionOfDone = prev.DefinitionOfDone
	}
	if len(in.TaskExpectedFilesOrComponents) == 0 {
		in.TaskExpectedFilesOrComponents = prev.ExpectedFilesOrComponents
	}
	if len(in.AffectedSymbolsAndFiles) == 0 {
		in.AffectedSymbolsAndFiles = in.Previous.AffectedSymbolsAndFiles
	}
	if len(in.RequiredTests) == 0 {
		in.RequiredTests = in.Previous.RequiredTests
	}
	return in
}

// ReadYAML loads a Context previously written by WriteYAML/WriteToBundle
// from path, for a resuming build_task_context call to pass as
// BuildInput.Previous. found is false (with a nil error) when path does not
// exist yet, e.g. a task's first call.
func ReadYAML(path string) (c Context, found bool, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Context{}, false, nil
	}
	if err != nil {
		return Context{}, false, fmt.Errorf("taskcontext: read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Context{}, false, fmt.Errorf("taskcontext: decode %s: %w", path, err)
	}
	return c, true, nil
}

// ReadFromBundle is ReadYAML for a task's task.yaml evidence file within
// bundle, mirroring WriteToBundle.
func ReadFromBundle(bundle *evidence.Bundle) (Context, bool, error) {
	return ReadYAML(bundle.Path("task.yaml"))
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

// Caps bounding each store-derived context list (punokawan-d9v). These are
// deliberately generous - a task's genuinely relevant background rarely runs
// to dozens of contracts or decisions - but finite, so a large knowledge store
// cannot balloon a single task's execution context without limit.
const (
	maxRelevantSourceExcerpts = 20
	maxRelatedDecisions       = 20
	maxKnownConstraints       = 20
)

// appendScopedSummaries appends summaries of recs to dst, skipping records
// whose scope conflicts with the parent requirement's scope, until dst reaches
// cap. It preserves whatever dst already holds (so a shared cap can span
// several ListByType calls, and caller-supplied entries are never dropped).
func appendScopedSummaries(dst []string, recs []protocol.KnowledgeRecord, reqScope *protocol.KnowledgeRecordScope, cap int) []string {
	for _, r := range recs {
		if len(dst) >= cap {
			break
		}
		if !inRequirementScope(r.Scope, reqScope) {
			continue
		}
		dst = append(dst, summarize(r))
	}
	return dst
}

// inRequirementScope reports whether a record with recScope is plausibly
// relevant to a task under reqScope. It only excludes a record that explicitly
// declares a different project/repository/module than the requirement's;
// unscoped (cross-cutting) records, and any record when the requirement itself
// is unscoped, are kept - the goal is to drop obviously out-of-scope records,
// not to demand an exact scope match the schema rarely carries.
func inRequirementScope(recScope, reqScope *protocol.KnowledgeRecordScope) bool {
	if reqScope == nil || recScope == nil {
		return true
	}
	if scopeConflict(recScope.Project, reqScope.Project) {
		return false
	}
	if scopeConflict(recScope.Repository, reqScope.Repository) {
		return false
	}
	if scopeConflict(recScope.Module, reqScope.Module) {
		return false
	}
	return true
}

// scopeConflict reports whether a and b are both set to non-empty, differing
// values. A nil or empty side never conflicts (it is simply unspecified).
func scopeConflict(a, b *string) bool {
	return a != nil && b != nil && *a != "" && *b != "" && *a != *b
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
