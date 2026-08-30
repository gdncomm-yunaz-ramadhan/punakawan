package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func newSpoolTestStore(t *testing.T) *Store {
	t.Helper()
	return newTelemetryStore(t)
}

// TestSpoolProcessDeathAfterRenameBeforeIngestionIsRecoveredByDrain
// simulates the exact failure the spool exists for: WriteSpoolRecord
// completes (the record is durably on disk, renamed into place) but the
// process dies before it ever attempts ingestion. A later DrainSpool pass
// must still pick the file up and apply it.
func TestSpoolProcessDeathAfterRenameBeforeIngestionIsRecoveredByDrain(t *testing.T) {
	dataDir := t.TempDir()
	store := newSpoolTestStore(t)
	session := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "codex", ExternalSessionID: "thr-1"})

	rec := SpoolRecord{
		EventID:    "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ClientKind: "codex",
		EventName:  "Stop",
		Snapshot:   &SnapshotRequest{SessionID: session.ID, SourceID: "main", Sequence: 1, InputTokens: 7, OutputTokens: 3},
	}
	if err := WriteSpoolRecord(dataDir, rec); err != nil {
		t.Fatalf("WriteSpoolRecord: %v", err)
	}
	// Process "dies" here: no ingestion attempted yet, only the spool file
	// exists.

	drained, err := DrainSpool(context.Background(), dataDir, store)
	if err != nil {
		t.Fatalf("DrainSpool: %v", err)
	}
	if drained != 1 {
		t.Fatalf("drained = %d, want 1", drained)
	}
	if got := mustTotals(t, store, "d1").Counters.InputTokens; got != 7 {
		t.Fatalf("input tokens = %d, want 7 (recovered from spool)", got)
	}
	remaining, err := PendingSpoolFiles(dataDir)
	if err != nil {
		t.Fatalf("PendingSpoolFiles: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining spool files = %v, want none after a successful drain", remaining)
	}
}

// TestSpoolDuplicateDeliveryIsANoOp covers the file being drained twice
// (e.g. a retry racing a drain pass that already succeeded but had not yet
// removed the file, or a caller re-running "attempt immediate ingestion"
// after DrainSpool already handled it): the second ingest of the same
// monotonic snapshot must not double-count.
func TestSpoolDuplicateDeliveryIsANoOp(t *testing.T) {
	store := newSpoolTestStore(t)
	session := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "codex", ExternalSessionID: "thr-1"})
	rec := SpoolRecord{
		EventID:  "01ARZ3NDEKTSV4RRFFQ69G5FB0",
		Snapshot: &SnapshotRequest{SessionID: session.ID, SourceID: "main", Sequence: 1, InputTokens: 7},
	}
	if err := rec.Ingest(context.Background(), store); err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if err := rec.Ingest(context.Background(), store); err != nil {
		t.Fatalf("second (duplicate) ingest: %v", err)
	}
	if got := mustTotals(t, store, "d1").Counters.InputTokens; got != 7 {
		t.Fatalf("input tokens = %d, want 7 (duplicate delivery must not double-count)", got)
	}
}

// TestSpoolMalformedFileIsQuarantinedNotBlocking asserts a malformed spool
// file is moved aside rather than repeatedly failing (or blocking) the
// drain of every healthy file after it.
func TestSpoolMalformedFileIsQuarantinedNotBlocking(t *testing.T) {
	dataDir := t.TempDir()
	store := newSpoolTestStore(t)
	session := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "codex", ExternalSessionID: "thr-1"})

	dir, err := SpoolDir(dataDir)
	if err != nil {
		t.Fatalf("SpoolDir: %v", err)
	}
	// "01a..." sorts before "01b..." below, so the malformed file is drained
	// first, proving it does not block the healthy one that follows.
	if err := os.WriteFile(filepath.Join(dir, "01a-malformed.json"), []byte("not json"), 0o600); err != nil {
		t.Fatalf("write malformed spool file: %v", err)
	}
	healthy := SpoolRecord{EventID: "01b-healthy", Snapshot: &SnapshotRequest{SessionID: session.ID, SourceID: "main", Sequence: 1, InputTokens: 9}}
	if err := WriteSpoolRecord(dataDir, healthy); err != nil {
		t.Fatalf("WriteSpoolRecord: %v", err)
	}

	drained, err := DrainSpool(context.Background(), dataDir, store)
	if err != nil {
		t.Fatalf("DrainSpool: %v", err)
	}
	if drained != 1 {
		t.Fatalf("drained = %d, want 1 (the healthy file only)", drained)
	}
	if got := mustTotals(t, store, "d1").Counters.InputTokens; got != 9 {
		t.Fatalf("input tokens = %d, want 9", got)
	}
	quarantined, err := os.ReadDir(filepath.Join(dataDir, quarantineDirName))
	if err != nil {
		t.Fatalf("read quarantine dir: %v", err)
	}
	if len(quarantined) != 1 || quarantined[0].Name() != "01a-malformed.json" {
		t.Fatalf("quarantined files = %v, want exactly the malformed file", quarantined)
	}
	remaining, err := PendingSpoolFiles(dataDir)
	if err != nil {
		t.Fatalf("PendingSpoolFiles: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining spool files = %v, want none (malformed quarantined, healthy drained)", remaining)
	}
}

// TestSpoolTwoConcurrentWritersEachSurviveDrain writes two distinct events
// from two goroutines (standing in for two concurrent hook invocations)
// and asserts both survive and drain cleanly - concurrent writers must not
// clobber or lose each other's spool files.
func TestSpoolTwoConcurrentWritersEachSurviveDrain(t *testing.T) {
	dataDir := t.TempDir()
	store := newSpoolTestStore(t)
	a := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "codex", ExternalSessionID: "thr-a"})
	b := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "claude-code", ExternalSessionID: "thr-b"})

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = WriteSpoolRecord(dataDir, SpoolRecord{EventID: "01-writer-a", Snapshot: &SnapshotRequest{SessionID: a.ID, SourceID: "main", Sequence: 1, InputTokens: 4}})
	}()
	go func() {
		defer wg.Done()
		errs[1] = WriteSpoolRecord(dataDir, SpoolRecord{EventID: "02-writer-b", Snapshot: &SnapshotRequest{SessionID: b.ID, SourceID: "main", Sequence: 1, InputTokens: 5}})
	}()
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent writer %d: %v", i, err)
		}
	}

	files, err := PendingSpoolFiles(dataDir)
	if err != nil {
		t.Fatalf("PendingSpoolFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("pending spool files = %v, want both concurrent writers' files present", files)
	}

	drained, err := DrainSpool(context.Background(), dataDir, store)
	if err != nil {
		t.Fatalf("DrainSpool: %v", err)
	}
	if drained != 2 {
		t.Fatalf("drained = %d, want 2", drained)
	}
	if got := mustTotals(t, store, "d1").Counters.InputTokens; got != 9 {
		t.Fatalf("input tokens = %d, want 9 (4 + 5, both writers' events applied)", got)
	}
}

func TestWriteSpoolRecordRejectsEmptyEventID(t *testing.T) {
	if err := WriteSpoolRecord(t.TempDir(), SpoolRecord{}); err == nil {
		t.Fatal("expected an error for an empty event_id")
	}
}
