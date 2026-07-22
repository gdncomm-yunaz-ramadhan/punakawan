package evidence

import (
	"os"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestLedgerAppendAndForTask(t *testing.T) {
	root := t.TempDir()
	ledger, err := OpenLedger(root, "run-1")
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}

	taskA, taskB := "task-a", "task-b"
	now := time.Now().UTC()
	records := []protocol.EvidenceRecord{
		{Id: "ev-1", RunId: "run-1", TaskId: &taskA, Type: protocol.EvidenceRecordTypeGitDiff, CreatedAt: now},
		{Id: "ev-2", RunId: "run-1", TaskId: &taskB, Type: protocol.EvidenceRecordTypeTestReport, CreatedAt: now},
		{Id: "ev-3", RunId: "run-1", TaskId: &taskA, Type: protocol.EvidenceRecordTypeTestReport, CreatedAt: now},
	}
	for _, rec := range records {
		if err := ledger.Append(rec); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	got, err := ledger.ForTask(taskA)
	if err != nil {
		t.Fatalf("ForTask: %v", err)
	}
	if len(got) != 2 || got[0].Id != "ev-1" || got[1].Id != "ev-3" {
		t.Fatalf("ForTask(%q) = %+v, want [ev-1, ev-3] in append order", taskA, got)
	}
}

func TestLedgerForTaskOnEmpty(t *testing.T) {
	ledger, err := OpenLedger(t.TempDir(), "run-empty")
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	got, err := ledger.ForTask("task-a")
	if err != nil {
		t.Fatalf("ForTask: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for an empty ledger, got %+v", got)
	}
}

func TestRecordArtifactHashesFileAndAppends(t *testing.T) {
	root := t.TempDir()
	bundle, err := NewBundle(root, "run-1", "task-a")
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}
	if err := os.WriteFile(bundle.Path("diff.patch"), []byte("diff --git a b\n"), 0o644); err != nil {
		t.Fatalf("write diff.patch: %v", err)
	}

	ledger, err := OpenLedger(root, "run-1")
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	now := time.Now().UTC()
	rec, err := RecordArtifact(ledger, "run-1", "task-a", protocol.EvidenceRecordTypeGitDiff, bundle, "diff.patch", now)
	if err != nil {
		t.Fatalf("RecordArtifact: %v", err)
	}
	if rec.RunId != "run-1" || rec.TaskId == nil || *rec.TaskId != "task-a" || rec.Type != protocol.EvidenceRecordTypeGitDiff {
		t.Fatalf("RecordArtifact result = %+v, want run/task/type set", rec)
	}
	if rec.ContentHash == nil || !now.Equal(rec.CreatedAt) {
		t.Fatalf("RecordArtifact result = %+v, want a content hash and CreatedAt == now", rec)
	}

	got, err := ledger.ForTask("task-a")
	if err != nil {
		t.Fatalf("ForTask: %v", err)
	}
	if len(got) != 1 || got[0].Id != rec.Id {
		t.Fatalf("ForTask = %+v, want the just-recorded artifact", got)
	}
}

func TestRecordArtifactErrorsOnMissingFile(t *testing.T) {
	root := t.TempDir()
	bundle, err := NewBundle(root, "run-1", "task-a")
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}
	ledger, err := OpenLedger(root, "run-1")
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	if _, err := RecordArtifact(ledger, "run-1", "task-a", protocol.EvidenceRecordTypeGitDiff, bundle, "does-not-exist.patch", time.Now().UTC()); err == nil {
		t.Fatal("expected an error recording an artifact that was never written")
	}
}
