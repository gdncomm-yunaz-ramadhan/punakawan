package mcpserver

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/internal/workflowdef"
)

func TestPrepareWorkContextDefinitionAware(t *testing.T) {
	a := newTestApp(t)

	// A definition that requires metadata the project does not have.
	store, err := workflowdef.Open(a.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(workflowdef.Definition{
		Version:          workflowdef.SchemaVersion,
		ID:               "orient",
		Name:             "Repository Orientation",
		Enabled:          true,
		RequiredMetadata: []string{"test.command"},
	}); err != nil {
		t.Fatalf("save definition: %v", err)
	}

	h := prepareWorkContextHandler(a)
	_, out, err := h(context.Background(), nil, PrepareWorkContextInput{WorkflowId: "orient", Objective: "orient me"})
	if err != nil {
		t.Fatalf("prepare_work_context: %v", err)
	}
	if out.RunId == "" || out.Digest == "" {
		t.Fatalf("expected run id + digest, got %+v", out)
	}
	if out.WorkflowId != "orient" || out.AdHoc {
		t.Fatalf("expected definition-aware run, got %+v", out)
	}
	if len(out.Missing) != 1 || out.Missing[0].Key != "test.command" {
		t.Fatalf("expected missing required metadata, got %+v", out.Missing)
	}
	if out.State != "awaiting-clarification" {
		t.Fatalf("missing metadata should put run in awaiting-clarification, got %q", out.State)
	}

	// The run is persisted and carries the definition_ref + context snapshot.
	run, err := a.Workflow.Get(out.RunId)
	if err != nil {
		t.Fatalf("run not persisted: %v", err)
	}
	if run.DefinitionRef == nil || run.DefinitionRef.Id != "orient" {
		t.Fatalf("run missing definition_ref: %+v", run.DefinitionRef)
	}
	if run.ContextSnapshot == nil || run.ContextSnapshot.Digest == nil || *run.ContextSnapshot.Digest != out.Digest {
		t.Fatalf("run missing context snapshot digest")
	}
}

func TestPrepareWorkContextAmbiguousSelector(t *testing.T) {
	a := newTestApp(t)
	store, _ := workflowdef.Open(a.Workspace.Root)
	sel := []workflowdef.Selector{{Capability: "cap", Intent: "do"}}
	for _, id := range []string{"x", "y"} {
		if _, err := store.Save(workflowdef.Definition{Version: workflowdef.SchemaVersion, ID: id, Name: id, Enabled: true, Selectors: sel}); err != nil {
			t.Fatal(err)
		}
	}
	h := prepareWorkContextHandler(a)
	_, out, err := h(context.Background(), nil, PrepareWorkContextInput{Capability: "cap", Intent: "do"})
	if err != nil {
		t.Fatalf("ambiguous should be returned as candidates, not error: %v", err)
	}
	if len(out.Candidates) != 2 || out.RunId != "" {
		t.Fatalf("expected 2 candidates and no run, got %+v", out)
	}
}

func TestPrepareWorkContextAdHoc(t *testing.T) {
	a := newTestApp(t)
	h := prepareWorkContextHandler(a)
	_, out, err := h(context.Background(), nil, PrepareWorkContextInput{Objective: "just do it"})
	if err != nil {
		t.Fatalf("ad hoc prepare: %v", err)
	}
	if !out.AdHoc || out.RunId == "" {
		t.Fatalf("expected ad hoc run, got %+v", out)
	}
}
