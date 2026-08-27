package delivery

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRecordWorkLogPersistsTaskBoundLedgerAndView(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch, err := s.CreateOrchestration(ctx, "create-worklog-orch", NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	project := registerProject(t, s, "worklog-project")
	lane, err := s.CreateLane(ctx, "create-worklog-lane", NewID(), orch.Id, project.Id, "")
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	started := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)

	entry, err := s.RecordWorkLog(ctx, "record-worklog", "worklog-1", orch.Id, lane.Id, "session-1", "PAY-1901", started, 900, "Implemented retry policy")
	if err != nil {
		t.Fatalf("RecordWorkLog: %v", err)
	}
	if entry.JiraIssueKey != "PAY-1901" || entry.DurationSeconds != 900 || entry.SyncStatus != "pending" {
		t.Fatalf("entry = %+v, want pending task-bound 900-second worklog", entry)
	}

	duplicate, err := s.RecordWorkLog(ctx, "record-worklog", "worklog-1", orch.Id, lane.Id, "session-1", "PAY-1901", started, 900, "Implemented retry policy")
	if err != nil {
		t.Fatalf("idempotent RecordWorkLog: %v", err)
	}
	if duplicate.ID != entry.ID {
		t.Fatalf("duplicate id = %q, want %q", duplicate.ID, entry.ID)
	}

	view, err := s.BuildDeliveryView(ctx, orch.Id)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if len(view.WorkLogs) != 1 || view.WorkLogs[0].ID != entry.ID || view.WorkLogSeconds != 900 {
		t.Fatalf("worklog view = %+v, want one 900-second entry", view)
	}
	if view.LatestSeq != 2 {
		t.Fatalf("LatestSeq = %d, want worklog event reflected at sequence 2", view.LatestSeq)
	}
}

func TestRecordWorkLogRejectsMissingJiraTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch, err := s.CreateOrchestration(ctx, "create-missing-task", NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	project := registerProject(t, s, "missing-task-project")
	lane, err := s.CreateLane(ctx, "create-missing-task-lane", NewID(), orch.Id, project.Id, "")
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	if _, err := s.RecordWorkLog(ctx, "missing-jira-task", "worklog-2", orch.Id, lane.Id, "", "", time.Now(), 60, "work"); err == nil {
		t.Fatal("RecordWorkLog accepted a missing exact Jira task")
	}
}

func TestRequiredJiraWorkLogBlocksLeaseCompletionUntilRecorded(t *testing.T) {
	s := NewStore(newTestDB(t), WithRequiredJiraWorkLogs())
	ctx := context.Background()
	orch, err := s.CreateOrchestration(ctx, "create-required-worklog", NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	if _, err := s.CaptureRequirement(ctx, "capture-jira", orch.Id, SourceInput{Provider: "jira", ExternalID: "PAY-1842", Title: "Retry policy"}); err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
	project := registerProject(t, s, "required-worklog-project")
	task := createTestTask(t, s, orch.Id, "retry policy")
	if _, err := s.RouteParentTask(ctx, "route-required-worklog", orch.Id, task.Id, project.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	lane, err := s.CreateLane(ctx, "create-required-worklog-lane", NewID(), orch.Id, project.Id, task.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	if _, err := s.SyncFrontier(ctx, "sync-required-worklog", orch.Id); err != nil {
		t.Fatalf("SyncFrontier: %v", err)
	}
	runnable, err := s.GetLane(ctx, orch.Id, lane.Id)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	leased, err := s.GrantLease(ctx, "lease-required-worklog", orch.Id, lane.Id, runnable.Revision, "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("GrantLease: %v", err)
	}
	if _, err := s.CompleteLease(ctx, "complete-without-worklog", orch.Id, lane.Id, *leased.LeaseToken, leased.Revision); !errors.Is(err, ErrWorkLogRequired) {
		t.Fatalf("CompleteLease without worklog = %v, want ErrWorkLogRequired", err)
	}
	if _, err := s.RecordWorkLog(ctx, "record-required-worklog", "worklog-required", orch.Id, lane.Id, "session-1", "PAY-1901", time.Now().UTC(), 60, "Implemented retry policy"); err != nil {
		t.Fatalf("RecordWorkLog: %v", err)
	}
	current, err := s.GetLane(ctx, orch.Id, lane.Id)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	if _, err := s.CompleteLease(ctx, "complete-with-worklog", orch.Id, lane.Id, *current.LeaseToken, current.Revision); err != nil {
		t.Fatalf("CompleteLease after worklog: %v", err)
	}
}
