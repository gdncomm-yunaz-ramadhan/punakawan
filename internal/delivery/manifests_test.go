package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/ygrip/punakawan/internal/worklogalloc"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestApprovalManifestLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "manifest-project")
	task := createTestTask(t, s, orch.Id, "task")
	if _, err := s.RouteParentTask(ctx, "route", orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}

	detail := "git found on PATH"
	plan := ManifestPlan{
		PlannedBaseRef:  "main",
		PlannedBranches: []string{"feature/x"},
		ExpectsCommits:  true,
		ExpectsPushes:   true,
		Checks:          []protocol.PreflightCheck{{Name: "executable:git", Status: protocol.PreflightCheckStatusPass, Classification: protocol.PreflightCheckClassificationRequired, Detail: &detail}},
	}
	manifest, err := s.CreateApprovalManifest(ctx, "manifest-1", NewID(), orch.Id, proj.Id, []string{task.Id}, plan)
	if err != nil {
		t.Fatalf("CreateApprovalManifest: %v", err)
	}
	if manifest.Status != protocol.ApprovalManifestStatusPending {
		t.Fatalf("expected pending status, got %s", manifest.Status)
	}
	if len(manifest.Checks) != 1 || manifest.Checks[0].Name != "executable:git" {
		t.Fatalf("expected checks snapshot preserved, got %+v", manifest.Checks)
	}

	if _, err := s.ApproveManifest(ctx, "approve-agent", orch.Id, manifest.Id, "semar"); err == nil {
		t.Fatal("expected agent-role self-approval to be rejected")
	}

	approved, err := s.ApproveManifest(ctx, "approve-1", orch.Id, manifest.Id, "a-human-reviewer")
	if err != nil {
		t.Fatalf("ApproveManifest: %v", err)
	}
	if approved.Status != protocol.ApprovalManifestStatusApproved || approved.ApprovedBy == nil || *approved.ApprovedBy != "a-human-reviewer" {
		t.Fatalf("unexpected approved manifest: %+v", approved)
	}
	if approved.DecidedAt == nil {
		t.Fatal("expected decided_at to be set")
	}

	if _, err := s.ApproveManifest(ctx, "approve-2", orch.Id, manifest.Id, "a-human-reviewer"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState re-deciding an already-approved manifest, got %v", err)
	}
}

// TestApprovalManifestCarriesProposedWorklog proves a manifest created with
// a non-empty worklogalloc.Allocation makes that proposed dev/test/review
// split visible on the manifest itself, before any
// approval decision - GetApprovalManifest (not just the just-created return
// value) must also reflect it, since a human reviewing before approving
// reads the manifest back rather than trusting the creator's local copy.
func TestApprovalManifestCarriesProposedWorklog(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "manifest-worklog-project")
	task := createTestTask(t, s, orch.Id, "task")
	if _, err := s.RouteParentTask(ctx, "route-worklog", orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}

	alloc := worklogalloc.Allocate(6, []worklogalloc.Subtask{
		{Key: "PROJ-2", Summary: "Development"},
		{Key: "PROJ-3", Summary: "Testing"},
	})
	if len(alloc.Worklogs) != 2 || alloc.UnmappedHours != 0 {
		t.Fatalf("test setup: expected both buckets matched with nothing unmapped, got %+v", alloc)
	}

	plan := ManifestPlan{PlannedBaseRef: "main", ProposedWorklog: alloc}
	created, err := s.CreateApprovalManifest(ctx, "manifest-worklog", NewID(), orch.Id, proj.Id, []string{task.Id}, plan)
	if err != nil {
		t.Fatalf("CreateApprovalManifest: %v", err)
	}

	for _, m := range []*protocol.ApprovalManifest{created, mustGetManifest(t, s, orch.Id, created.Id)} {
		if m.ProposedWorklogTotalHours == nil || *m.ProposedWorklogTotalHours != 6 {
			t.Fatalf("expected ProposedWorklogTotalHours=6, got %v", m.ProposedWorklogTotalHours)
		}
		if m.ProposedWorklogUnmappedHours != nil {
			t.Fatalf("expected no unmapped hours, got %v", *m.ProposedWorklogUnmappedHours)
		}
		if len(m.ProposedWorklog) != 2 {
			t.Fatalf("expected 2 proposed worklog entries, got %+v", m.ProposedWorklog)
		}
		for _, w := range m.ProposedWorklog {
			if w.Hours != 3 {
				t.Fatalf("expected an even 3h split per matched bucket, got %+v", w)
			}
		}
	}
}

// TestApprovalManifestOmitsProposedWorklogWhenNotGiven proves a manifest
// created with the zero-value ManifestPlan.ProposedWorklog (the common case
// for a caller with no test-run evidence yet) reports no proposed hours at
// all rather than a misleading zero, per worklogalloc.Allocate's own
// zero-hours contract.
func TestApprovalManifestOmitsProposedWorklogWhenNotGiven(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "manifest-no-worklog-project")
	task := createTestTask(t, s, orch.Id, "task")
	if _, err := s.RouteParentTask(ctx, "route-no-worklog", orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}

	manifest, err := s.CreateApprovalManifest(ctx, "manifest-no-worklog", NewID(), orch.Id, proj.Id, []string{task.Id}, ManifestPlan{PlannedBaseRef: "main"})
	if err != nil {
		t.Fatalf("CreateApprovalManifest: %v", err)
	}
	if manifest.ProposedWorklogTotalHours != nil {
		t.Fatalf("expected no proposed worklog total, got %v", *manifest.ProposedWorklogTotalHours)
	}
	if len(manifest.ProposedWorklog) != 0 {
		t.Fatalf("expected no proposed worklog entries, got %+v", manifest.ProposedWorklog)
	}
}

func mustGetManifest(t *testing.T, s *Store, orchestrationID, manifestID string) *protocol.ApprovalManifest {
	t.Helper()
	m, err := s.GetApprovalManifest(context.Background(), orchestrationID, manifestID)
	if err != nil {
		t.Fatalf("GetApprovalManifest: %v", err)
	}
	return m
}

func TestApproveManifestBlockedByFailedRequiredCheckNotByOptional(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "manifest-gate-project")
	task := createTestTask(t, s, orch.Id, "task")
	if _, err := s.RouteParentTask(ctx, "route", orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}

	failDetail := "git not found on PATH"
	manifest, err := s.CreateApprovalManifest(ctx, "manifest-gate", NewID(), orch.Id, proj.Id, []string{task.Id}, ManifestPlan{
		PlannedBaseRef: "main",
		Checks: []protocol.PreflightCheck{
			{Name: "executable:git", Status: protocol.PreflightCheckStatusFail, Classification: protocol.PreflightCheckClassificationRequired, Detail: &failDetail},
			{Name: "external-service:optional-thing", Status: protocol.PreflightCheckStatusSkipped, Classification: protocol.PreflightCheckClassificationOptional},
		},
	})
	if err != nil {
		t.Fatalf("CreateApprovalManifest: %v", err)
	}

	if _, err := s.ApproveManifest(ctx, "approve-blocked", orch.Id, manifest.Id, "a-human-reviewer"); !errors.Is(err, ErrRequiredCheckFailed) {
		t.Fatalf("expected ErrRequiredCheckFailed when a required check failed, got %v", err)
	}
}

func TestApprovalManifestRejectsTaskFromDifferentProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	projA := registerProject(t, s, "manifest-a")
	projB := registerProject(t, s, "manifest-b")
	taskA := createTestTask(t, s, orch.Id, "task-a")
	if _, err := s.RouteParentTask(ctx, "route-a", orch.Id, taskA.Id, projA.Id); err != nil {
		t.Fatalf("route a: %v", err)
	}

	if _, err := s.CreateApprovalManifest(ctx, "manifest-mismatch", NewID(), orch.Id, projB.Id, []string{taskA.Id}, ManifestPlan{PlannedBaseRef: "main"}); !errors.Is(err, ErrScopeMismatch) {
		t.Fatalf("expected ErrScopeMismatch for a task routed to a different project, got %v", err)
	}
}
