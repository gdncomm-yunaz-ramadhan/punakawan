package workflow

import (
	"errors"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestStoreAppendListCurrent(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	run := New("run-1", "checkout-platform", protocol.WorkflowRunWorkflowNameFeatureDelivery, now)
	if err := store.Append(run); err != nil {
		t.Fatalf("Append: %v", err)
	}

	run, err = Advance(run, protocol.WorkflowRunStateContextBuilding, "", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if err := store.Append(run); err != nil {
		t.Fatalf("Append: %v", err)
	}

	all, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List returned %d entries, want 2", len(all))
	}

	current, err := store.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	got, ok := current["run-1"]
	if !ok {
		t.Fatal("Current missing run-1")
	}
	if got.State != protocol.WorkflowRunStateContextBuilding {
		t.Fatalf("Current state = %q, want context-building", got.State)
	}
	if len(got.Checkpoints) != 2 {
		t.Fatalf("Current checkpoints = %+v, want 2 entries", got.Checkpoints)
	}
}

func TestStoreGetNotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := store.Get("does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
