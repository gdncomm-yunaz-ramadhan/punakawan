package workflow

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestNewCreatesInitialCheckpoint(t *testing.T) {
	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	run := New("run-1", "checkout-platform", protocol.WorkflowRunWorkflowNameFeatureDelivery, now)

	if run.State != protocol.WorkflowRunStateCreated {
		t.Fatalf("State = %q, want created", run.State)
	}
	if len(run.Checkpoints) != 1 || run.Checkpoints[0].State != string(protocol.WorkflowRunStateCreated) {
		t.Fatalf("Checkpoints = %+v, want one created checkpoint", run.Checkpoints)
	}
}

func TestAdvanceAppendsCheckpointAndUpdatesState(t *testing.T) {
	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	run := New("run-1", "checkout-platform", protocol.WorkflowRunWorkflowNameFeatureDelivery, now)

	later := now.Add(time.Minute)
	run, err := Advance(run, protocol.WorkflowRunStateContextBuilding, "collecting sources", later)
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if run.State != protocol.WorkflowRunStateContextBuilding {
		t.Fatalf("State = %q, want context-building", run.State)
	}
	if len(run.Checkpoints) != 2 {
		t.Fatalf("Checkpoints = %+v, want 2 entries", run.Checkpoints)
	}
	if run.Checkpoints[1].Note == nil || *run.Checkpoints[1].Note != "collecting sources" {
		t.Fatalf("Checkpoints[1].Note = %v, want set", run.Checkpoints[1].Note)
	}
	if !run.UpdatedAt.Equal(later) {
		t.Fatalf("UpdatedAt = %v, want %v", run.UpdatedAt, later)
	}
}

func TestAdvanceRejectsUnknownState(t *testing.T) {
	now := time.Now().UTC()
	run := New("run-1", "checkout-platform", protocol.WorkflowRunWorkflowNameFeatureDelivery, now)

	if _, err := Advance(run, protocol.WorkflowRunState("bogus"), "", now); err == nil {
		t.Fatal("expected error for unknown state")
	}
}

func TestAdvanceRejectsLeavingTerminalState(t *testing.T) {
	now := time.Now().UTC()
	run := New("run-1", "checkout-platform", protocol.WorkflowRunWorkflowNameFeatureDelivery, now)
	run, err := Advance(run, protocol.WorkflowRunStateCompleted, "", now)
	if err != nil {
		t.Fatalf("Advance to completed: %v", err)
	}

	if _, err := Advance(run, protocol.WorkflowRunStateExecuting, "", now); err == nil {
		t.Fatal("expected error advancing out of a terminal state")
	}
}

func TestIsTerminal(t *testing.T) {
	for _, s := range []protocol.WorkflowRunState{
		protocol.WorkflowRunStateCompleted,
		protocol.WorkflowRunStateFailed,
		protocol.WorkflowRunStateCancelled,
	} {
		if !IsTerminal(s) {
			t.Errorf("IsTerminal(%q) = false, want true", s)
		}
	}
	if IsTerminal(protocol.WorkflowRunStateExecuting) {
		t.Error("IsTerminal(executing) = true, want false")
	}
}
