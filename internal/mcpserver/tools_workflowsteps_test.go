package mcpserver

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/internal/workflow"
	"github.com/ygrip/punakawan/internal/workflowdef"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func saveStepDef(t *testing.T, root string) {
	t.Helper()
	store, err := workflowdef.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(workflowdef.Definition{
		Version: workflowdef.SchemaVersion,
		ID:      "steps",
		Name:    "Two Step",
		Enabled: true,
		Steps: []workflowdef.Step{
			{ID: "a", Capability: "write_file"},
			{ID: "b", Capability: "run_tests", InputFrom: []string{"a"}},
		},
	}); err != nil {
		t.Fatalf("save def: %v", err)
	}
}

func TestWorkflowStepLifecycleAndResume(t *testing.T) {
	a := newTestApp(t)
	saveStepDef(t, a.Workspace.Root)

	// Prepare a definition-aware run.
	_, prep, err := prepareWorkContextHandler(a)(context.Background(), nil, PrepareWorkContextInput{WorkflowId: "steps"})
	if err != nil {
		t.Fatal(err)
	}
	runID := prep.RunId
	if runID == "" {
		t.Fatal("no run id")
	}

	// get_next: a ready, b pending.
	_, next, err := getNextWorkflowStepHandler(a)(context.Background(), nil, GetNextWorkflowStepInput{RunId: runID})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Ready) != 1 || next.Ready[0].StepID != "a" {
		t.Fatalf("expected step a ready, got %+v", next.Ready)
	}
	if len(next.Blocked) != 1 || next.Blocked[0].StepID != "b" {
		t.Fatalf("expected step b blocked, got %+v", next.Blocked)
	}

	// Complete a with evidence -> b unlocks.
	_, after, err := completeWorkflowStepHandler(a)(context.Background(), nil, CompleteWorkflowStepInput{RunId: runID, StepId: "a", EvidenceIds: []string{"ev-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Ready) != 1 || after.Ready[0].StepID != "b" {
		t.Fatalf("step b should be ready after a: %+v", after.Ready)
	}

	// Complete b -> all done.
	_, done, err := completeWorkflowStepHandler(a)(context.Background(), nil, CompleteWorkflowStepInput{RunId: runID, StepId: "b", EvidenceIds: []string{"ev-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if !done.AllComplete {
		t.Fatalf("expected all_complete, got %+v", done)
	}

	// Record outcome.
	_, _, err = recordWorkOutcomeHandler(a)(context.Background(), nil, RecordWorkOutcomeInput{
		RunId: runID, Status: "success", Summary: "done",
		Observations: []struct {
			Kind        string   `json:"kind" jsonschema:"workflow|metadata|knowledge|contradiction|workflow-revision"`
			Summary     string   `json:"summary"`
			EvidenceIds []string `json:"evidence_ids,omitempty"`
		}{{Kind: "workflow", Summary: "these two steps always run together"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Resume-after-restart: a fresh store over the same root retains step
	// state and the outcome (agent-context plan Phase 3 exit criterion).
	fresh, err := workflow.Open(a.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	run, err := fresh.Get(runID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	states := map[string]protocol.WorkflowRunStepProgressElemState{}
	for _, sp := range run.StepProgress {
		states[sp.StepId] = sp.State
	}
	if states["a"] != protocol.WorkflowRunStepProgressElemStateCompleted || states["b"] != protocol.WorkflowRunStepProgressElemStateCompleted {
		t.Fatalf("step state lost across restart: %+v", states)
	}
	if run.Outcome == nil || run.Outcome.Status != protocol.WorkflowRunOutcomeStatusSuccess {
		t.Fatalf("outcome lost across restart: %+v", run.Outcome)
	}
	if len(run.Outcome.Observations) != 1 {
		t.Fatalf("observation not persisted")
	}

	// Capability events recorded for the run (structured trace).
	events, err := workflow.OpenEvents(a.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	evs, err := events.ForRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("expected 2 capability events, got %d", len(evs))
	}
}

func TestCompleteStepRejectsAdHocRun(t *testing.T) {
	a := newTestApp(t)
	_, prep, err := prepareWorkContextHandler(a)(context.Background(), nil, PrepareWorkContextInput{Objective: "ad hoc"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = completeWorkflowStepHandler(a)(context.Background(), nil, CompleteWorkflowStepInput{RunId: prep.RunId, StepId: "x", EvidenceIds: []string{"e"}})
	if err == nil {
		t.Fatal("completing a step on an ad hoc run should error")
	}
}
