package delivery

import (
	"context"
	"errors"
	"testing"

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
