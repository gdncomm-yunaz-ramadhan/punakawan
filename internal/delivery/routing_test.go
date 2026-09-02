package delivery

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// TestExactBindingRoutesDeterministicallyAmbiguousDoesNot covers
// acceptance criterion 2's routing half: exact repository/slug evidence
// resolves to exactly one project; anything else is reported ambiguous
// rather than guessed at.
func TestExactBindingRoutesDeterministicallyAmbiguousDoesNot(t *testing.T) {
	s := newTestStore(t)
	a := registerProject(t, s, "checkout-platform")
	b := registerProject(t, s, "billing-platform")
	projects := []*protocol.DeliveryProject{a, b}

	id, ok := Resolve(RouteEvidence{RepositoryURL: a.RepositoryUrl}, projects)
	if !ok || id != a.Id {
		t.Fatalf("exact repository match = (%q, %v), want (%q, true)", id, ok, a.Id)
	}

	a.RepositoryUrl = "https://github.com/acme/checkout-platform.git"
	id, ok = Resolve(RouteEvidence{RepositoryURL: "git@github.com:acme/checkout-platform.git"}, projects)
	if !ok || id != a.Id {
		t.Fatalf("equivalent SSH repository match = (%q, %v), want (%q, true)", id, ok, a.Id)
	}

	id, ok = Resolve(RouteEvidence{Slug: "billing-platform"}, projects)
	if !ok || id != b.Id {
		t.Fatalf("exact slug match = (%q, %v), want (%q, true)", id, ok, b.Id)
	}

	if _, ok := Resolve(RouteEvidence{}, projects); ok {
		t.Fatal("empty evidence must be ambiguous, not routed")
	}
	if _, ok := Resolve(RouteEvidence{RepositoryURL: "https://example.test/unknown.git"}, projects); ok {
		t.Fatal("non-matching evidence must be ambiguous, not routed")
	}
}

func TestRouteParentTaskRejectsUnknownAndInactiveProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	task := createTestTask(t, s, orch.Id, "task")

	if _, err := s.RouteParentTask(ctx, "r1", orch.Id, task.Id, "does-not-exist"); err == nil {
		t.Fatal("expected error routing to unknown project")
	}

	disabled := registerProject(t, s, "disabled-route-target")
	if err := s.disableProjectForTest(ctx, disabled.Id); err != nil {
		t.Fatalf("disable project: %v", err)
	}
	if _, err := s.RouteParentTask(ctx, "r2", orch.Id, task.Id, disabled.Id); err != ErrProjectInactive {
		t.Fatalf("expected ErrProjectInactive routing to disabled project, got %v", err)
	}
}
