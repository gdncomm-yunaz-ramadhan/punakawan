package workflow

import (
	"errors"
	"os"
	"path/filepath"
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

// TestStoreIncrementalReadPicksUpAppendsAcrossCalls exercises the
// offset-tracked cache: each call must see exactly what has been appended
// so far, decoding only the new bytes, not the whole
// file - Current/List correctness must not depend on how many times they
// were called in between.
func TestStoreIncrementalReadPicksUpAppendsAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	runA := New("run-a", "checkout-platform", protocol.WorkflowRunWorkflowNameFeatureDelivery, now)
	if err := store.Append(runA); err != nil {
		t.Fatalf("Append run-a: %v", err)
	}

	current, err := store.Current()
	if err != nil {
		t.Fatalf("Current (1st): %v", err)
	}
	if len(current) != 1 {
		t.Fatalf("Current (1st) = %+v, want 1 run", current)
	}

	runB := New("run-b", "checkout-platform", protocol.WorkflowRunWorkflowNameFeatureDelivery, now)
	if err := store.Append(runB); err != nil {
		t.Fatalf("Append run-b: %v", err)
	}
	runA, err = Advance(runA, protocol.WorkflowRunStateContextBuilding, "", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Advance run-a: %v", err)
	}
	if err := store.Append(runA); err != nil {
		t.Fatalf("Append run-a (2nd): %v", err)
	}

	current, err = store.Current()
	if err != nil {
		t.Fatalf("Current (2nd): %v", err)
	}
	if len(current) != 2 {
		t.Fatalf("Current (2nd) = %+v, want 2 runs", current)
	}
	if current["run-a"].State != protocol.WorkflowRunStateContextBuilding {
		t.Fatalf("run-a state = %q, want context-building (the latest append)", current["run-a"].State)
	}
	if current["run-b"].State != runB.State {
		t.Fatalf("run-b state = %q, want %q", current["run-b"].State, runB.State)
	}

	all, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("List = %d entries, want 3 (every append, not folded)", len(all))
	}
}

// TestStoreIncrementalReadRecoversFromTruncatedFile guards the readOffset
// shrink-detection path: if runs.jsonl is ever smaller than what this Store
// already read (deleted and recreated, e.g. by an external tool), the next
// read must rescan from the start rather than seeking past EOF or missing
// data.
func TestStoreIncrementalReadRecoversFromTruncatedFile(t *testing.T) {
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
	if _, err := store.Current(); err != nil {
		t.Fatalf("Current (1st): %v", err)
	}

	runsPath := filepath.Join(dir, ".punakawan", "workflow", "runs.jsonl")
	if err := os.Remove(runsPath); err != nil {
		t.Fatalf("remove runs.jsonl: %v", err)
	}

	replacement := New("run-2", "checkout-platform", protocol.WorkflowRunWorkflowNameFeatureDelivery, now)
	if err := store.Append(replacement); err != nil {
		t.Fatalf("Append after truncation: %v", err)
	}

	current, err := store.Current()
	if err != nil {
		t.Fatalf("Current (after truncation): %v", err)
	}
	if len(current) != 1 {
		t.Fatalf("Current (after truncation) = %+v, want exactly run-2", current)
	}
	if _, ok := current["run-2"]; !ok {
		t.Fatalf("Current (after truncation) missing run-2: %+v", current)
	}
}

// TestStoreIncrementalReadSeenByASeparateStoreInstance confirms the cache is
// purely a same-process read optimization, not a source of staleness across
// processes: a second Store instance opened against the same directory must
// see everything the first one appended.
func TestStoreIncrementalReadSeenByASeparateStoreInstance(t *testing.T) {
	dir := t.TempDir()
	writer, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (writer): %v", err)
	}
	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	run := New("run-1", "checkout-platform", protocol.WorkflowRunWorkflowNameFeatureDelivery, now)
	if err := writer.Append(run); err != nil {
		t.Fatalf("Append: %v", err)
	}

	reader, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (reader): %v", err)
	}
	current, err := reader.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if _, ok := current["run-1"]; !ok {
		t.Fatalf("a fresh Store instance did not see the other instance's append: %+v", current)
	}
}
