package workflow

import (
	"os"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store
}

func appendRaw(t *testing.T, path string, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(data); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

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
	// Set the terminal state directly rather than via Advance: this test
	// is about refusing to leave a terminal state, not about which paths
	// reach one (TestAdvanceFollowsFullHappyPath covers that).
	run.State = protocol.WorkflowRunStateCompleted

	if _, err := Advance(run, protocol.WorkflowRunStateExecuting, "", now); err == nil {
		t.Fatal("expected error advancing out of a terminal state")
	}
}

func TestAdvanceRejectsSkippingIntermediateStages(t *testing.T) {
	now := time.Now().UTC()
	run := New("run-1", "checkout-platform", protocol.WorkflowRunWorkflowNameFeatureDelivery, now)

	if _, err := Advance(run, protocol.WorkflowRunStateCompleted, "", now); err == nil {
		t.Fatal("expected error jumping from created straight to completed, skipping every intermediate stage")
	}
	if _, err := Advance(run, protocol.WorkflowRunStateExecuting, "", now); err == nil {
		t.Fatal("expected error jumping from created straight to executing")
	}
}

func TestAdvanceFollowsFullHappyPath(t *testing.T) {
	now := time.Now().UTC()
	run := New("run-1", "checkout-platform", protocol.WorkflowRunWorkflowNameFeatureDelivery, now)

	path := []protocol.WorkflowRunState{
		protocol.WorkflowRunStateContextBuilding,
		protocol.WorkflowRunStatePlanning,
		protocol.WorkflowRunStateAwaitingApproval,
		protocol.WorkflowRunStateExecuting,
		protocol.WorkflowRunStateReviewing,
		protocol.WorkflowRunStateCompleted,
	}
	var err error
	for _, next := range path {
		run, err = Advance(run, next, "", now)
		if err != nil {
			t.Fatalf("Advance to %s: %v", next, err)
		}
	}
	if run.State != protocol.WorkflowRunStateCompleted {
		t.Fatalf("State = %q, want completed", run.State)
	}
}

func TestAdvanceAllowsClarificationLoopBack(t *testing.T) {
	now := time.Now().UTC()
	run := New("run-1", "checkout-platform", protocol.WorkflowRunWorkflowNameFeatureDelivery, now)

	run, err := Advance(run, protocol.WorkflowRunStateContextBuilding, "", now)
	if err != nil {
		t.Fatalf("Advance to context-building: %v", err)
	}
	run, err = Advance(run, protocol.WorkflowRunStateAwaitingClarification, "", now)
	if err != nil {
		t.Fatalf("Advance to awaiting-clarification: %v", err)
	}
	if _, err := Advance(run, protocol.WorkflowRunStateContextBuilding, "", now); err != nil {
		t.Fatalf("expected the clarification loop back to context-building to be allowed: %v", err)
	}
}

func TestAdvanceAllowsBlockedEscapeHatchAndResume(t *testing.T) {
	now := time.Now().UTC()
	run := New("run-1", "checkout-platform", protocol.WorkflowRunWorkflowNameFeatureDelivery, now)

	run, err := Advance(run, protocol.WorkflowRunStateBlocked, "", now)
	if err != nil {
		t.Fatalf("expected blocked to be reachable from created: %v", err)
	}
	if _, err := Advance(run, protocol.WorkflowRunStateExecuting, "", now); err != nil {
		t.Fatalf("expected resuming from blocked into executing to be allowed: %v", err)
	}
}

func TestRefreshDoesNotCacheUnterminatedJSONLine(t *testing.T) {
	store := newTestStore(t)
	appendRaw(t, store.path, `{"id":"run-1"`)
	if _, err := store.Current(); err != nil {
		t.Fatalf("partial trailing record must be deferred, not decoded: %v", err)
	}
	appendRaw(t, store.path, `,"workspace":"ws","workflow_name":"feature-delivery","state":"created","created_at":"2026-08-29T00:00:00Z","updated_at":"2026-08-29T00:00:00Z"}`+"\n")
	got, err := store.Current()
	if err != nil || got["run-1"].Id != "run-1" {
		t.Fatalf("completed record was not recovered: got=%v err=%v", got, err)
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
