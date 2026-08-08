package delivery

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func createTestTask(t *testing.T, s *Store, orchID, title string) *protocol.ParentTask {
	t.Helper()
	ctx := context.Background()
	src, err := s.CaptureRequirement(ctx, "cap-"+NewID(), orchID, SourceInput{Provider: "freetext", Title: title})
	if err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
	task, err := s.CreateParentTask(ctx, "task-"+NewID(), NewID(), orchID, title, []string{src.Id})
	if err != nil {
		t.Fatalf("CreateParentTask: %v", err)
	}
	return task
}

// TestDeterministicAcyclicGraphWithEvidence covers acceptance criterion
// 4: a repository/requirement fixture produces a graph edge carrying
// type, evidence, confidence, and origin, and computing it twice from
// the same events is deterministic.
func TestDeterministicAcyclicGraphWithEvidence(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	migration := createTestTask(t, s, orch.Id, "migration")
	api := createTestTask(t, s, orch.Id, "api")

	edge, err := s.AddDependencyEdge(ctx, "edge-1", NewID(), orch.Id, api.Id, migration.Id,
		protocol.DependencyEdgeTypeRequires, protocol.DependencyEdgeOriginExplicitSource, 0.95, "api task explicitly blocked on migration in Jira")
	if err != nil {
		t.Fatalf("AddDependencyEdge: %v", err)
	}
	if edge.Type != protocol.DependencyEdgeTypeRequires || edge.Origin != protocol.DependencyEdgeOriginExplicitSource || edge.Confidence != 0.95 {
		t.Fatalf("edge fields not preserved: %+v", edge)
	}
	if edge.Evidence == nil || *edge.Evidence == "" {
		t.Fatalf("expected evidence to be recorded, got %+v", edge)
	}

	first, err := s.GetDependencyEdge(ctx, orch.Id, edge.Id)
	if err != nil {
		t.Fatalf("GetDependencyEdge (1st): %v", err)
	}
	second, err := s.GetDependencyEdge(ctx, orch.Id, edge.Id)
	if err != nil {
		t.Fatalf("GetDependencyEdge (2nd): %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("non-deterministic edge replay: first=%+v second=%+v", first, second)
	}
}

// TestCycleRejectedWithoutMutation covers acceptance criterion 6's
// cycle-creation half: adding an edge that would close a cycle fails
// closed and does not record anything.
func TestCycleRejectedWithoutMutation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	a := createTestTask(t, s, orch.Id, "a")
	b := createTestTask(t, s, orch.Id, "b")

	if _, err := s.AddDependencyEdge(ctx, "e1", NewID(), orch.Id, a.Id, b.Id, protocol.DependencyEdgeTypeRequires, protocol.DependencyEdgeOriginUser, 1, "a needs b"); err != nil {
		t.Fatalf("AddDependencyEdge a->b: %v", err)
	}
	if _, err := s.AddDependencyEdge(ctx, "e2", NewID(), orch.Id, b.Id, a.Id, protocol.DependencyEdgeTypeRequires, protocol.DependencyEdgeOriginUser, 1, "b needs a"); !errors.Is(err, ErrCycle) {
		t.Fatalf("expected ErrCycle for b->a after a->b, got %v", err)
	}

	_, edges, err := s.ListGraph(ctx, orch.Id)
	if err != nil {
		t.Fatalf("ListGraph: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("rejected cycle edge must not be recorded, got %d edges", len(edges))
	}
}

// TestMissingNodeRejected covers the "missing nodes" half of acceptance
// criterion 6.
func TestMissingNodeRejected(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	a := createTestTask(t, s, orch.Id, "a")

	if _, err := s.AddDependencyEdge(ctx, "e1", NewID(), orch.Id, a.Id, "does-not-exist", protocol.DependencyEdgeTypeRequires, protocol.DependencyEdgeOriginUser, 1, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown to_task_id, got %v", err)
	}
}

// TestUnsafeCrossProjectEdgeRejected covers the cross-project half of
// acceptance criterion 6: an inferred edge between tasks routed to
// different projects fails closed, but an explicitly declared one
// succeeds.
func TestUnsafeCrossProjectEdgeRejected(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	projA := registerProject(t, s, "cross-a")
	projB := registerProject(t, s, "cross-b")
	a := createTestTask(t, s, orch.Id, "a")
	b := createTestTask(t, s, orch.Id, "b")

	if _, err := s.RouteParentTask(ctx, "route-a", orch.Id, a.Id, projA.Id); err != nil {
		t.Fatalf("route a: %v", err)
	}
	if _, err := s.RouteParentTask(ctx, "route-b", orch.Id, b.Id, projB.Id); err != nil {
		t.Fatalf("route b: %v", err)
	}

	if _, err := s.AddDependencyEdge(ctx, "e1", NewID(), orch.Id, a.Id, b.Id, protocol.DependencyEdgeTypeRequires, protocol.DependencyEdgeOriginModelInference, 0.5, "looks related"); !errors.Is(err, ErrUnsafeCrossProjectEdge) {
		t.Fatalf("expected ErrUnsafeCrossProjectEdge for inferred cross-project edge, got %v", err)
	}
	if _, err := s.AddDependencyEdge(ctx, "e2", NewID(), orch.Id, a.Id, b.Id, protocol.DependencyEdgeTypeRequires, protocol.DependencyEdgeOriginUser, 1, "user declared shared outcome"); err != nil {
		t.Fatalf("expected explicit-origin cross-project edge to succeed, got %v", err)
	}
}

// TestRemoveEdgeRequiresEvidenceOnceRouted covers acceptance criteria 3
// and 7: reorganization is free before routing, but removing an edge
// from a routed task requires evidence and the edge is never silently
// dropped without it.
func TestRemoveEdgeRequiresEvidenceOnceRouted(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "remove-evidence")
	a := createTestTask(t, s, orch.Id, "a")
	b := createTestTask(t, s, orch.Id, "b")

	edge, err := s.AddDependencyEdge(ctx, "e1", NewID(), orch.Id, a.Id, b.Id, protocol.DependencyEdgeTypeRequires, protocol.DependencyEdgeOriginExplicitSource, 1, "explicit")
	if err != nil {
		t.Fatalf("AddDependencyEdge: %v", err)
	}

	if _, err := s.RouteParentTask(ctx, "route-a", orch.Id, a.Id, proj.Id); err != nil {
		t.Fatalf("route a: %v", err)
	}

	if _, err := s.RemoveDependencyEdge(ctx, "rm-1", orch.Id, edge.Id, ""); !errors.Is(err, ErrEvidenceRequired) {
		t.Fatalf("expected ErrEvidenceRequired removing edge from a routed task without evidence, got %v", err)
	}
	removed, err := s.RemoveDependencyEdge(ctx, "rm-2", orch.Id, edge.Id, "confirmed no longer needed after replanning")
	if err != nil {
		t.Fatalf("RemoveDependencyEdge with evidence: %v", err)
	}
	if removed.Status != protocol.DependencyEdgeStatusRemoved {
		t.Fatalf("expected edge status removed, got %s", removed.Status)
	}
}

// TestFrontierNamesUnresolvedPredecessors covers acceptance criterion 5.
func TestFrontierNamesUnresolvedPredecessors(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	migration := createTestTask(t, s, orch.Id, "migration")
	api := createTestTask(t, s, orch.Id, "api")
	docs := createTestTask(t, s, orch.Id, "docs") // independent of both

	if _, err := s.AddDependencyEdge(ctx, "e1", NewID(), orch.Id, api.Id, migration.Id, protocol.DependencyEdgeTypeRequires, protocol.DependencyEdgeOriginUser, 1, "x"); err != nil {
		t.Fatalf("AddDependencyEdge: %v", err)
	}

	tasks, edges, err := s.ListGraph(ctx, orch.Id)
	if err != nil {
		t.Fatalf("ListGraph: %v", err)
	}
	frontier, blocked := Frontier(tasks, edges, map[string]bool{})

	frontierSet := map[string]bool{}
	for _, id := range frontier {
		frontierSet[id] = true
	}
	if !frontierSet[migration.Id] || !frontierSet[docs.Id] {
		t.Fatalf("expected migration and docs (independent nodes) in frontier together, got %v", frontier)
	}
	if frontierSet[api.Id] {
		t.Fatalf("api should be blocked by migration, not in frontier: %v", frontier)
	}
	if preds := blocked[api.Id]; len(preds) != 1 || preds[0] != migration.Id {
		t.Fatalf("blocked[api] = %v, want [%s]", preds, migration.Id)
	}

	// Once migration resolves, api joins the frontier.
	frontier2, blocked2 := Frontier(tasks, edges, map[string]bool{migration.Id: true})
	frontierSet2 := map[string]bool{}
	for _, id := range frontier2 {
		frontierSet2[id] = true
	}
	if !frontierSet2[api.Id] {
		t.Fatalf("expected api in frontier once migration resolved, got %v", frontier2)
	}
	if _, stillBlocked := blocked2[api.Id]; stillBlocked {
		t.Fatalf("api must not still be reported blocked: %v", blocked2)
	}
}
