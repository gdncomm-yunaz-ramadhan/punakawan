package taskstore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/storage"
)

// newTestStore opens the shared SQLite storage kernel in a temp dir and
// scopes a Store to a fixed test project id.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, "test-project")
}

func TestCreateListGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.Create(ctx, CreateInput{Title: "Build the thing", Description: "do it", Type: "task", Priority: 2, Labels: []string{"backend"}, AcceptanceCriteria: []string{"works", "tested"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == "" {
		t.Fatal("empty id")
	}

	issues, ready, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("want 1 issue, got %d", len(issues))
	}
	if issues[0].Title != "Build the thing" || issues[0].Priority != 2 || issues[0].IssueType != "task" {
		t.Fatalf("unexpected issue: %+v", issues[0])
	}
	if len(issues[0].Labels) != 1 || issues[0].Labels[0] != "backend" {
		t.Fatalf("labels round-trip failed: %+v", issues[0].Labels)
	}
	// A lone open task with no blockers is ready.
	if !ready[id] {
		t.Fatalf("expected %s ready", id)
	}

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AcceptanceCriteria != "works\ntested" {
		t.Fatalf("acceptance round-trip: %q", got.AcceptanceCriteria)
	}
}

func TestDependencyBlocksReadiness(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	base, err := s.Create(ctx, CreateInput{Title: "base"})
	if err != nil {
		t.Fatalf("create base: %v", err)
	}
	dependent, err := s.Create(ctx, CreateInput{Title: "dependent"})
	if err != nil {
		t.Fatalf("create dependent: %v", err)
	}
	// dependent is blocked by base.
	if err := s.AddDependency(ctx, dependent, base, "blocks"); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}

	_, ready, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !ready[base] {
		t.Fatalf("base should be ready")
	}
	if ready[dependent] {
		t.Fatalf("dependent should be blocked by open base")
	}

	// Get should surface the edge both ways.
	dep, err := s.Get(ctx, dependent)
	if err != nil {
		t.Fatalf("Get dependent: %v", err)
	}
	if len(dep.Dependencies) != 1 || dep.Dependencies[0].ID != base {
		t.Fatalf("dependent should depend on base: %+v", dep.Dependencies)
	}
	b, err := s.Get(ctx, base)
	if err != nil {
		t.Fatalf("Get base: %v", err)
	}
	if len(b.Dependents) != 1 || b.Dependents[0].ID != dependent {
		t.Fatalf("base should have dependent: %+v", b.Dependents)
	}
}

func TestGetNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Get(context.Background(), "nope"); err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestProjectScopingPreventsLeakage(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	a := New(db, "project-a")
	b := New(db, "project-b")

	if _, err := a.Create(ctx, CreateInput{Title: "only in A"}); err != nil {
		t.Fatalf("create in A: %v", err)
	}

	issuesA, _, err := a.List(ctx)
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(issuesA) != 1 {
		t.Fatalf("project A: want 1 issue, got %d", len(issuesA))
	}

	issuesB, _, err := b.List(ctx)
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(issuesB) != 0 {
		t.Fatalf("project B must not see project A's tasks, got %d", len(issuesB))
	}
}
