package mcpserver

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ygrip/punakawan/internal/project"
	"github.com/ygrip/punakawan/internal/workflow"
	"github.com/ygrip/punakawan/internal/workflowdef"
)

// repoRoot locates the punakawan repo root from this test file, so the dogfood
// test can read the workflow definitions actually shipped in
// .punakawan/workflows/ rather than a fixture copy.
func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

// TestShippedWorkflowsValidateAgainstRegistry dogfoods the three real workflow
// definitions (agent-context plan §9 Phase 5): every step and allowed
// capability must resolve against the SAME capability registry the MCP server
// exposes. This is the anti-drift guarantee end to end — a shipped definition
// naming a capability the server does not register fails here.
func TestShippedWorkflowsValidateAgainstRegistry(t *testing.T) {
	store, err := workflowdef.Open(repoRoot())
	if err != nil {
		t.Fatal(err)
	}
	defs, err := store.List()
	if err != nil {
		t.Fatal(err)
	}

	caps := workflowdef.NewCapabilitySet(CapabilityRegistry(newTestApp(t)).Names(), nil)
	found := map[string]bool{}
	for _, d := range defs {
		found[d.ID] = true
		if err := workflowdef.Validate(d, caps); err != nil {
			t.Errorf("shipped workflow %q does not validate against the capability registry: %v", d.ID, err)
		}
	}
	for _, want := range []string{"repo-orientation", "implementation-with-tests", "pr-review"} {
		if !found[want] {
			t.Errorf("expected shipped workflow %q under .punakawan/workflows/", want)
		}
	}
}

// TestEndToEndContextLoop exercises the whole loop against a real app: prepare
// (missing metadata → awaiting-clarification), resolve the metadata, re-prepare,
// walk the steps, record the outcome, confirm completability, survive a
// restart, and turn a reusable observation into an accepted workflow revision.
func TestEndToEndContextLoop(t *testing.T) {
	a := newTestApp(t)
	store, _ := workflowdef.Open(a.Workspace.Root)
	def := workflowdef.Definition{
		Version: workflowdef.SchemaVersion, ID: "impl", Name: "Impl", Enabled: true, Revision: 1,
		RequiredMetadata: []string{"test.command"},
		Steps: []workflowdef.Step{
			{ID: "context", Capability: "build_task_context"},
			{ID: "implement", Capability: "write_file", InputFrom: []string{"context"}},
			{ID: "test", Capability: "run_tests", InputFrom: []string{"implement"}},
		},
	}
	if _, err := store.Save(def); err != nil {
		t.Fatal(err)
	}
	prepare := prepareWorkContextHandler(a)

	// 1. Missing required metadata → awaiting-clarification.
	_, out, err := prepare(context.Background(), nil, PrepareWorkContextInput{WorkflowId: "impl"})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "awaiting-clarification" || len(out.Missing) != 1 {
		t.Fatalf("expected awaiting-clarification with missing metadata, got %+v", out)
	}

	// 2. Resolve the metadata.
	proj, _ := project.Load(a.Workspace.Root)
	if err := proj.AddMetadata(project.MetadataEntry{Key: "test.command", Description: "how to test", Value: "go test ./..."}, proj.Revision); err != nil {
		t.Fatal(err)
	}
	if err := project.Save(a.Workspace.Root, proj, project.SaveOptions{Actor: "test", Action: "add", Key: "test.command"}); err != nil {
		t.Fatal(err)
	}

	// 3. Re-prepare: no missing context now.
	_, out2, err := prepare(context.Background(), nil, PrepareWorkContextInput{WorkflowId: "impl"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out2.Missing) != 0 {
		t.Fatalf("metadata resolved but still missing: %+v", out2.Missing)
	}
	runID := out2.RunId

	// 4. Walk the steps in dependency order.
	complete := completeWorkflowStepHandler(a)
	for _, step := range []string{"context", "implement", "test"} {
		if _, _, err := complete(context.Background(), nil, CompleteWorkflowStepInput{RunId: runID, StepId: step, EvidenceIds: []string{"ev-" + step}}); err != nil {
			t.Fatalf("complete %s: %v", step, err)
		}
	}

	// 5. Record the outcome and confirm the run is now completable.
	if _, _, err := recordWorkOutcomeHandler(a)(context.Background(), nil, RecordWorkOutcomeInput{RunId: runID, Status: "success", Summary: "done"}); err != nil {
		t.Fatal(err)
	}

	// 6. Restart recovery: a fresh store keeps step state + outcome.
	fresh, _ := workflow.Open(a.Workspace.Root)
	run, err := fresh.Get(runID)
	if err != nil {
		t.Fatal(err)
	}
	if err := workflow.CanComplete(run); err != nil {
		t.Fatalf("run should be completable after outcome + all steps: %v", err)
	}

	// 7. Turn a reusable observation into an accepted workflow revision.
	_, prop, err := proposeProjectLearningHandler(a)(context.Background(), nil, ProposeProjectLearningInput{
		ArtifactType: "workflow", TargetId: "impl",
		Candidate: map[string]any{"version": workflowdef.SchemaVersion, "id": "impl", "name": "Impl v2"},
		Rationale: "the last runs all added the same step", SourceRunIds: []string{runID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prop.ReviewId == "" {
		t.Fatal("expected a review-backed proposal")
	}
	// Canonical definition unchanged before acceptance.
	if cur, _ := store.Get("impl"); cur.Revision != 1 {
		t.Fatalf("proposal changed canonical before acceptance: rev %d", cur.Revision)
	}
}
