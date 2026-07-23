package sessionsummary

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func testRun() protocol.WorkflowRun {
	role := protocol.WorkflowRunActiveRolePetruk
	objective := "Add refund validation"
	initiator := "user"
	return protocol.WorkflowRun{
		Id:           "run-20260723-001",
		Workspace:    "checkout-platform",
		WorkflowName: protocol.WorkflowRunWorkflowNameFeatureDelivery,
		State:        protocol.WorkflowRunStateExecuting,
		CreatedAt:    time.Date(2026, 7, 23, 10, 12, 4, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 7, 23, 10, 13, 41, 0, time.UTC),
		ActiveRole:   &role,
		Objective:    &objective,
		Initiator:    &initiator,
	}
}

func TestBuildFillsCounts(t *testing.T) {
	open := 3
	counts := Counts{
		TaskCounts:    &protocol.PanelSessionSummaryTaskCounts{Open: &open},
		EvidenceCount: 12,
		WarningCount:  2,
		ErrorCount:    0,
	}

	summary := Build(testRun(), counts)

	if summary.Id != "run-20260723-001" {
		t.Fatalf("Id = %q, want run-20260723-001", summary.Id)
	}
	if summary.WorkspaceId != "checkout-platform" {
		t.Fatalf("WorkspaceId = %q, want checkout-platform", summary.WorkspaceId)
	}
	if summary.Status != "executing" {
		t.Fatalf("Status = %q, want executing", summary.Status)
	}
	if summary.ActiveRole == nil || *summary.ActiveRole != protocol.PanelSessionSummaryActiveRolePetruk {
		t.Fatalf("ActiveRole = %v, want petruk", summary.ActiveRole)
	}
	if summary.EvidenceCount == nil || *summary.EvidenceCount != 12 {
		t.Fatalf("EvidenceCount = %v, want 12", summary.EvidenceCount)
	}
	if summary.TaskCounts == nil || summary.TaskCounts.Open == nil || *summary.TaskCounts.Open != 3 {
		t.Fatalf("TaskCounts.Open = %v, want 3", summary.TaskCounts)
	}
}

func TestWriteYAMLReadYAMLRoundTrip(t *testing.T) {
	root := t.TempDir()
	summary := Build(testRun(), Counts{EvidenceCount: 1, WarningCount: 0, ErrorCount: 0})

	if err := WriteYAML(root, summary); err != nil {
		t.Fatalf("WriteYAML: %v", err)
	}

	got, err := ReadYAML(root, summary.Id)
	if err != nil {
		t.Fatalf("ReadYAML: %v", err)
	}
	if got.Id != summary.Id || got.Status != summary.Status || got.WorkspaceId != summary.WorkspaceId {
		t.Fatalf("ReadYAML = %+v, want %+v", got, summary)
	}
	if got.EvidenceCount == nil || *got.EvidenceCount != 1 {
		t.Fatalf("EvidenceCount = %v, want 1", got.EvidenceCount)
	}
}

func TestWriteYAMLOverwritesPreviousSummary(t *testing.T) {
	root := t.TempDir()
	run := testRun()

	if err := WriteYAML(root, Build(run, Counts{EvidenceCount: 1})); err != nil {
		t.Fatalf("WriteYAML (first): %v", err)
	}

	run.State = protocol.WorkflowRunStateCompleted
	if err := WriteYAML(root, Build(run, Counts{EvidenceCount: 5})); err != nil {
		t.Fatalf("WriteYAML (second): %v", err)
	}

	got, err := ReadYAML(root, run.Id)
	if err != nil {
		t.Fatalf("ReadYAML: %v", err)
	}
	if got.Status != "completed" {
		t.Fatalf("Status = %q, want completed", got.Status)
	}
	if got.EvidenceCount == nil || *got.EvidenceCount != 5 {
		t.Fatalf("EvidenceCount = %v, want 5", got.EvidenceCount)
	}
}

func TestReadYAMLMissingRunErrors(t *testing.T) {
	root := t.TempDir()
	if _, err := ReadYAML(root, "no-such-run"); err == nil {
		t.Fatal("expected an error reading a run with no summary.yaml")
	}
}
