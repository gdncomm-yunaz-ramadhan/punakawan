package workflow

import (
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func runWithSteps(states ...protocol.WorkflowRunStepProgressElemState) protocol.WorkflowRun {
	run := protocol.WorkflowRun{Id: "r", DefinitionRef: &protocol.WorkflowRunDefinitionRef{Id: "d"}}
	ids := []string{"a", "b"}
	for i, st := range states {
		run.StepProgress = append(run.StepProgress, protocol.WorkflowRunStepProgressElem{StepId: ids[i], State: st})
	}
	return run
}

var twoSteps = []StepDef{
	{ID: "a", Capability: "write_file"},
	{ID: "b", Capability: "run_tests", InputFrom: []string{"a"}},
}

func allowAll(string) bool { return true }

func TestNextStepsReadyAndPending(t *testing.T) {
	run := runWithSteps(protocol.WorkflowRunStepProgressElemStateReady, protocol.WorkflowRunStepProgressElemStatePending)
	ready, blocked := NextSteps(run, twoSteps, allowAll)
	if len(ready) != 1 || ready[0].StepID != "a" {
		t.Fatalf("step a should be ready: %+v", ready)
	}
	if len(blocked) != 1 || blocked[0].StepID != "b" || !strings.Contains(blocked[0].Reason, "waiting on") {
		t.Fatalf("step b should be pending on a: %+v", blocked)
	}
}

func TestNextStepsBlocksDisallowedCapability(t *testing.T) {
	run := runWithSteps(protocol.WorkflowRunStepProgressElemStateReady)
	deny := func(c string) bool { return c != "write_file" }
	ready, blocked := NextSteps(run, twoSteps[:1], deny)
	if len(ready) != 0 {
		t.Fatalf("disallowed capability must not be ready")
	}
	if len(blocked) != 1 || !strings.Contains(blocked[0].Reason, "not allowed") {
		t.Fatalf("expected blocked-by-capability, got %+v", blocked)
	}
}

func TestCompleteStepUnlocksDependent(t *testing.T) {
	run := runWithSteps(protocol.WorkflowRunStepProgressElemStateReady, protocol.WorkflowRunStepProgressElemStatePending)
	run, err := CompleteStep(run, "a", []string{"ev-1"}, "", twoSteps, allowAll, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	states := stepState(run)
	if states["a"] != protocol.WorkflowRunStepProgressElemStateCompleted {
		t.Fatalf("a should be completed")
	}
	if states["b"] != protocol.WorkflowRunStepProgressElemStateReady {
		t.Fatalf("b should be unlocked to ready, got %s", states["b"])
	}
}

func TestCompleteStepRequiresEvidenceOrDeviation(t *testing.T) {
	run := runWithSteps(protocol.WorkflowRunStepProgressElemStateReady)
	if _, err := CompleteStep(run, "a", nil, "", twoSteps[:1], allowAll, time.Now()); err == nil {
		t.Fatal("expected error completing with neither evidence nor deviation")
	}
	// Deviation reason alone is enough.
	if _, err := CompleteStep(run, "a", nil, "used a manual edit", twoSteps[:1], allowAll, time.Now()); err != nil {
		t.Fatalf("deviation-only completion should be allowed: %v", err)
	}
}

func TestCompleteStepRejectsDisallowedCapability(t *testing.T) {
	run := runWithSteps(protocol.WorkflowRunStepProgressElemStateReady)
	deny := func(string) bool { return false }
	if _, err := CompleteStep(run, "a", []string{"ev"}, "", twoSteps[:1], deny, time.Now()); err == nil {
		t.Fatal("expected disallowed-capability rejection")
	}
}

func TestCanCompleteGate(t *testing.T) {
	// Context-aware run without outcome cannot complete.
	run := runWithSteps(protocol.WorkflowRunStepProgressElemStateCompleted)
	if err := CanComplete(run); err == nil {
		t.Fatal("context-aware run without outcome should be blocked")
	}
	// With outcome but an incomplete step -> blocked (definition-backed).
	run = runWithSteps(protocol.WorkflowRunStepProgressElemStateReady)
	run = RecordOutcome(run, protocol.WorkflowRunOutcome{Status: protocol.WorkflowRunOutcomeStatusSuccess}, time.Now())
	if err := CanComplete(run); err == nil {
		t.Fatal("incomplete step should block completion")
	}
	// Outcome + all steps completed -> allowed.
	run = runWithSteps(protocol.WorkflowRunStepProgressElemStateCompleted)
	run = RecordOutcome(run, protocol.WorkflowRunOutcome{Status: protocol.WorkflowRunOutcomeStatusSuccess}, time.Now())
	if err := CanComplete(run); err != nil {
		t.Fatalf("should be completable: %v", err)
	}
	// A non-context-aware run is unaffected.
	if err := CanComplete(protocol.WorkflowRun{Id: "plain"}); err != nil {
		t.Fatalf("plain run should be unaffected: %v", err)
	}
}
