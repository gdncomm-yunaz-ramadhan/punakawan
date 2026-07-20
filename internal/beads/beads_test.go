package beads

import (
	"context"
	"os/exec"
	"testing"

	"github.com/ygrip/punakawan/internal/tools"
)

// newTestProject initializes a throwaway bd project rooted at t.TempDir(),
// mirroring internal/knowledge/service_test.go's newTestStore skip pattern
// for dolt. It never touches this repository's own .beads/ directory.
func newTestProject(t *testing.T) (*tools.Supervisor, string) {
	t.Helper()
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not installed")
	}

	dir := t.TempDir()
	sup := tools.New(dir)

	res, err := sup.Run(context.Background(), tools.Spec{
		Name: "bd",
		Args: []string{"init", "--non-interactive", "--prefix", "test", "--skip-agents", "--skip-hooks", "-q"},
		Dir:  dir,
	})
	if err != nil {
		t.Fatalf("bd init: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("bd init failed: %s", res.Stderr)
	}
	return sup, dir
}

func TestAvailable(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not installed")
	}

	dir := t.TempDir()
	sup := tools.New(dir)
	if !Available(context.Background(), sup, dir) {
		t.Fatal("expected Available to be true when bd is on PATH")
	}
}

func TestCreateTaskTitleRequired(t *testing.T) {
	dir := t.TempDir()
	sup := tools.New(dir)
	if _, err := CreateTask(context.Background(), sup, dir, "", "desc", CreateTaskOptions{}); err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestAddDependencyRequiresIDs(t *testing.T) {
	dir := t.TempDir()
	sup := tools.New(dir)
	if err := AddDependency(context.Background(), sup, dir, "", "bd-1", "blocks"); err == nil {
		t.Fatal("expected error for empty fromID")
	}
	if err := AddDependency(context.Background(), sup, dir, "bd-1", "", "blocks"); err == nil {
		t.Fatal("expected error for empty toID")
	}
}

func TestCreateTaskAndDependency(t *testing.T) {
	sup, dir := newTestProject(t)
	ctx := context.Background()

	epicID, err := CreateTask(ctx, sup, dir, "Feature epic", "epic description", CreateTaskOptions{Type: "epic"})
	if err != nil {
		t.Fatalf("CreateTask (epic): %v", err)
	}
	if epicID == "" {
		t.Fatal("expected non-empty epic id")
	}

	taskID, err := CreateTask(ctx, sup, dir, "Confirm data model", "task description", CreateTaskOptions{
		Type:               "task",
		Parent:             epicID,
		Labels:             []string{"beads-integration", "requirement"},
		AcceptanceCriteria: []string{"criterion one", "criterion two"},
		ExternalRef:        "jira-PAY-1842",
	})
	if err != nil {
		t.Fatalf("CreateTask (child): %v", err)
	}
	if taskID == "" {
		t.Fatal("expected non-empty task id")
	}

	secondID, err := CreateTask(ctx, sup, dir, "Create database migration", "second task", CreateTaskOptions{
		Type:   "task",
		Parent: epicID,
	})
	if err != nil {
		t.Fatalf("CreateTask (second child): %v", err)
	}

	if err := AddDependency(ctx, sup, dir, secondID, taskID, "blocks"); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}
}
