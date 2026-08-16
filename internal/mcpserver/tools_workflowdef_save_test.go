package mcpserver

import (
	"context"
	"errors"
	"testing"

	"github.com/ygrip/punakawan/internal/workflowdef"
)

func TestSaveWorkflowDefinitionCreatesNewDefinition(t *testing.T) {
	a := newTestApp(t)
	h := saveWorkflowDefinitionHandler(a, CapabilityRegistry(a))

	_, out, err := h(context.Background(), nil, SaveWorkflowDefinitionInput{
		Definition: workflowdef.Definition{
			ID:      "user-dictated-flow",
			Name:    "User Dictated Flow",
			Enabled: true,
			Steps: []workflowdef.Step{
				{ID: "orient", Capability: "build_context_dossier"},
			},
		},
	})
	if err != nil {
		t.Fatalf("save_workflow_definition: %v", err)
	}
	if out.Action != "created" || out.Revision != 1 || out.Id != "user-dictated-flow" {
		t.Fatalf("unexpected output: %+v", out)
	}

	store, err := workflowdef.Open(a.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	def, err := store.Get("user-dictated-flow")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if def.Version != workflowdef.SchemaVersion {
		t.Fatalf("expected version defaulted to %q, got %q", workflowdef.SchemaVersion, def.Version)
	}
}

func TestSaveWorkflowDefinitionUpdatesWithMatchingRevision(t *testing.T) {
	a := newTestApp(t)
	h := saveWorkflowDefinitionHandler(a, CapabilityRegistry(a))

	def := workflowdef.Definition{
		ID:      "iterate-flow",
		Name:    "Iterate Flow",
		Enabled: true,
		Steps:   []workflowdef.Step{{ID: "orient", Capability: "build_context_dossier"}},
	}
	_, first, err := h(context.Background(), nil, SaveWorkflowDefinitionInput{Definition: def})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	def.Revision = first.Revision
	def.Description = "revised"
	_, second, err := h(context.Background(), nil, SaveWorkflowDefinitionInput{Definition: def})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if second.Action != "updated" || second.Revision != 2 {
		t.Fatalf("unexpected output: %+v", second)
	}
}

func TestSaveWorkflowDefinitionRejectsStaleRevision(t *testing.T) {
	a := newTestApp(t)
	h := saveWorkflowDefinitionHandler(a, CapabilityRegistry(a))

	def := workflowdef.Definition{
		ID:      "conflict-flow",
		Name:    "Conflict Flow",
		Enabled: true,
		Steps:   []workflowdef.Step{{ID: "orient", Capability: "build_context_dossier"}},
	}
	if _, _, err := h(context.Background(), nil, SaveWorkflowDefinitionInput{Definition: def}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// def.Revision is still 0 (stale) - a second save with the same stale
	// revision must be rejected, not silently accepted as another create.
	_, _, err := h(context.Background(), nil, SaveWorkflowDefinitionInput{Definition: def})
	if err == nil || !errors.Is(err, workflowdef.ErrRevisionConflict) {
		t.Fatalf("expected ErrRevisionConflict, got %v", err)
	}
}

func TestSaveWorkflowDefinitionRejectsUnknownCapability(t *testing.T) {
	a := newTestApp(t)
	h := saveWorkflowDefinitionHandler(a, CapabilityRegistry(a))

	_, _, err := h(context.Background(), nil, SaveWorkflowDefinitionInput{
		Definition: workflowdef.Definition{
			ID:      "bad-flow",
			Name:    "Bad Flow",
			Enabled: true,
			Steps:   []workflowdef.Step{{ID: "s1", Capability: "not_a_real_capability"}},
		},
	})
	if err == nil || !errors.Is(err, workflowdef.ErrUnknownCapability) {
		t.Fatalf("expected ErrUnknownCapability, got %v", err)
	}
}

// TestSaveWorkflowDefinitionIsImmediatelySelectorResolvable proves end to
// end that a saved definition with a selector is resolvable by the very
// next prepare_work_context call, with no separate publish/reload/
// panel-accept step.
func TestSaveWorkflowDefinitionIsImmediatelySelectorResolvable(t *testing.T) {
	a := newTestApp(t)
	saveHandler := saveWorkflowDefinitionHandler(a, CapabilityRegistry(a))

	_, _, err := saveHandler(context.Background(), nil, SaveWorkflowDefinitionInput{
		Definition: workflowdef.Definition{
			ID:      "dictated-orient",
			Name:    "Dictated Orientation",
			Enabled: true,
			Selectors: []workflowdef.Selector{
				{Capability: "repository", Intent: "orient"},
			},
			Steps: []workflowdef.Step{
				{ID: "orient", Capability: "build_context_dossier"},
			},
		},
	})
	if err != nil {
		t.Fatalf("save_workflow_definition: %v", err)
	}

	prepare := prepareWorkContextHandler(a)
	_, out, err := prepare(context.Background(), nil, PrepareWorkContextInput{
		Capability: "repository",
		Intent:     "orient",
		Objective:  "orient in this repo",
	})
	if err != nil {
		t.Fatalf("prepare_work_context: %v", err)
	}
	if out.WorkflowId != "dictated-orient" || out.AdHoc {
		t.Fatalf("expected the freshly saved definition to resolve by selector, got %+v", out)
	}
}

func TestSaveWorkflowDefinitionRejectsJudgmentWithoutRationale(t *testing.T) {
	a := newTestApp(t)
	h := saveWorkflowDefinitionHandler(a, CapabilityRegistry(a))

	_, _, err := h(context.Background(), nil, SaveWorkflowDefinitionInput{
		Definition: workflowdef.Definition{
			ID:      "no-rationale-flow",
			Name:    "No Rationale Flow",
			Enabled: true,
			Steps:   []workflowdef.Step{{ID: "s1", Capability: "build_context_dossier"}},
		},
		Judgment: &WorkflowJudgment{},
	})
	if err == nil {
		t.Fatal("expected an error when judgment.rationale is empty")
	}

	store, storeErr := workflowdef.Open(a.Workspace.Root)
	if storeErr != nil {
		t.Fatal(storeErr)
	}
	if _, getErr := store.Get("no-rationale-flow"); !errors.Is(getErr, workflowdef.ErrNotFound) {
		t.Fatal("a rejected judgment call must not have saved the definition either")
	}
}

// TestSaveWorkflowDefinitionRecordsAgentJudgmentAsAcceptedProposal proves
// an agent-judgment save applies immediately (no panel click) while still
// recording a fingerprinted, already-accepted learning proposal.
func TestSaveWorkflowDefinitionRecordsAgentJudgmentAsAcceptedProposal(t *testing.T) {
	a := newTestApp(t)
	h := saveWorkflowDefinitionHandler(a, CapabilityRegistry(a))

	_, out, err := h(context.Background(), nil, SaveWorkflowDefinitionInput{
		Definition: workflowdef.Definition{
			ID:      "judged-flow",
			Name:    "Judged Flow",
			Enabled: true,
			Steps:   []workflowdef.Step{{ID: "s1", Capability: "build_context_dossier", Intent: "assemble"}},
		},
		Judgment: &WorkflowJudgment{Rationale: "this exact context+review sequence recurred three times"},
	})
	if err != nil {
		t.Fatalf("save_workflow_definition: %v", err)
	}
	if out.Action != "created" {
		t.Fatalf("expected the definition to be applied immediately, got %+v", out)
	}
	if out.ProposalId == "" || out.SupportCount != 1 {
		t.Fatalf("expected a new proposal at support_count=1, got %+v", out)
	}

	// Not left pending - it already happened.
	store, err := workflowdef.Open(a.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("judged-flow"); err != nil {
		t.Fatalf("expected the definition to already be live, got: %v", err)
	}
}

func TestSaveWorkflowDefinitionJudgmentDedupsByFingerprintAndIncrementsSupportCount(t *testing.T) {
	a := newTestApp(t)
	h := saveWorkflowDefinitionHandler(a, CapabilityRegistry(a))

	steps := []workflowdef.Step{{ID: "s1", Capability: "build_context_dossier", Intent: "assemble"}}
	_, first, err := h(context.Background(), nil, SaveWorkflowDefinitionInput{
		Definition: workflowdef.Definition{ID: "recurring-flow-a", Name: "Recurring Flow A", Enabled: true, Steps: steps},
		Judgment:   &WorkflowJudgment{Rationale: "first time seeing this pattern"},
	})
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	if first.SupportCount != 1 {
		t.Fatalf("expected support_count=1 on first capture, got %d", first.SupportCount)
	}

	// A different definition id, but the SAME step capability:intent
	// sequence - the fingerprint is step-graph based, not id based, so this
	// must reinforce the same proposal rather than opening a second one.
	_, second, err := h(context.Background(), nil, SaveWorkflowDefinitionInput{
		Definition: workflowdef.Definition{ID: "recurring-flow-b", Name: "Recurring Flow B", Enabled: true, Steps: steps},
		Judgment:   &WorkflowJudgment{Rationale: "seeing this pattern again"},
	})
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if second.ProposalId != first.ProposalId {
		t.Fatalf("expected the same proposal id to be reinforced, got %q vs %q", second.ProposalId, first.ProposalId)
	}
	if second.SupportCount != 2 {
		t.Fatalf("expected support_count=2 after the second capture, got %d", second.SupportCount)
	}
}
