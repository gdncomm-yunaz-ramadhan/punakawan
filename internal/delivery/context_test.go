package delivery

import (
	"context"
	"errors"
	"testing"
)

func TestBuildLaneContextAssemblesPinnedState(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "context-project")

	source, err := s.CaptureRequirement(ctx, "cap-1", orch.Id, SourceInput{Provider: "jira", ExternalID: "CTX-1", Title: "seed requirement"})
	if err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
	task, err := s.CreateParentTask(ctx, "task-1", NewID(), orch.Id, "context task", []string{source.Id})
	if err != nil {
		t.Fatalf("CreateParentTask: %v", err)
	}
	if _, err := s.RouteParentTask(ctx, "route-1", orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	if _, err := s.SetDeliveryProfile(ctx, "profile-1", NewID(), proj.Id, ProfileInput{
		LocalPath:  "/tmp/context-project",
		BaseBranch: "main",
	}); err != nil {
		t.Fatalf("SetDeliveryProfile: %v", err)
	}
	lane, err := s.CreateLane(ctx, "lane-1", NewID(), orch.Id, proj.Id, task.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}

	lc, err := s.BuildLaneContext(ctx, orch.Id, lane.Id)
	if err != nil {
		t.Fatalf("BuildLaneContext: %v", err)
	}
	if lc.Lane.Id != lane.Id {
		t.Fatalf("expected lane %s, got %s", lane.Id, lc.Lane.Id)
	}
	if lc.ParentTask.Id != task.Id {
		t.Fatalf("expected parent task %s, got %s", task.Id, lc.ParentTask.Id)
	}
	if len(lc.Sources) != 1 || lc.Sources[0].Id != source.Id {
		t.Fatalf("expected exactly the pinned source %s, got %+v", source.Id, lc.Sources)
	}
	if lc.Profile.ProjectId != proj.Id {
		t.Fatalf("expected profile for project %s, got %+v", proj.Id, lc.Profile)
	}
	if lc.Digest == "" {
		t.Fatal("expected a non-empty digest")
	}

	again, err := s.BuildLaneContext(ctx, orch.Id, lane.Id)
	if err != nil {
		t.Fatalf("BuildLaneContext (again): %v", err)
	}
	if again.Digest != lc.Digest {
		t.Fatalf("expected the same digest for the same pinned state, got %q and %q", lc.Digest, again.Digest)
	}

	// Changing the profile's revision (a second SetDeliveryProfile call)
	// must change the digest, since the pinned state actually changed.
	if _, err := s.SetDeliveryProfile(ctx, "profile-2", NewID(), proj.Id, ProfileInput{
		LocalPath:  "/tmp/context-project",
		BaseBranch: "develop",
	}); err != nil {
		t.Fatalf("SetDeliveryProfile (update): %v", err)
	}
	changed, err := s.BuildLaneContext(ctx, orch.Id, lane.Id)
	if err != nil {
		t.Fatalf("BuildLaneContext (after profile change): %v", err)
	}
	if changed.Digest == lc.Digest {
		t.Fatal("expected the digest to change after the profile revision changed")
	}
}

// TestBuildLaneContextFailsClosedWithoutDeliveryProfile checks that
// BuildLaneContext refuses to return a partial context - if the
// project has no delivery profile configured yet, it fails closed
// rather than returning a context with that piece silently missing.
func TestBuildLaneContextFailsClosedWithoutDeliveryProfile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "no-profile-project")

	source, err := s.CaptureRequirement(ctx, "cap-1", orch.Id, SourceInput{Provider: "jira", ExternalID: "CTX-2", Title: "seed requirement"})
	if err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
	task, err := s.CreateParentTask(ctx, "task-1", NewID(), orch.Id, "unprofiled task", []string{source.Id})
	if err != nil {
		t.Fatalf("CreateParentTask: %v", err)
	}
	lane, err := s.CreateLane(ctx, "lane-1", NewID(), orch.Id, proj.Id, task.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}

	if _, err := s.BuildLaneContext(ctx, orch.Id, lane.Id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound (no delivery profile yet), got %v", err)
	}
}
