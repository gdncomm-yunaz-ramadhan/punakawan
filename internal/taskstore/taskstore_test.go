package taskstore

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/tools"
)

// newTestStore opens a real Dolt-backed knowledge store (the taskstore shares
// its connection) in a temp dir, mirroring internal/knowledge's own test
// bootstrap.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}
	dir := t.TempDir()
	sup := tools.New(dir)
	k, err := knowledge.Open(sup, filepath.Join(dir, "knowledge"))
	if err != nil {
		t.Fatalf("knowledge.Open: %v", err)
	}
	t.Cleanup(func() { _ = k.Close() })

	s := New(k.DB())
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s
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
