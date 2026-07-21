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

func TestReadyNoIssues(t *testing.T) {
	sup, dir := newTestProject(t)
	ctx := context.Background()

	issues, err := Ready(ctx, sup, dir, ReadyOptions{})
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no ready issues in an empty project, got %d", len(issues))
	}
}

func TestReadyListsOpenIssue(t *testing.T) {
	sup, dir := newTestProject(t)
	ctx := context.Background()

	taskID, err := CreateTask(ctx, sup, dir, "Ready task", "task description", CreateTaskOptions{
		Labels: []string{"alpha"},
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	issues, err := Ready(ctx, sup, dir, ReadyOptions{})
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected exactly 1 ready issue, got %d: %+v", len(issues), issues)
	}
	if issues[0].ID != taskID {
		t.Fatalf("expected ready issue id %q, got %q", taskID, issues[0].ID)
	}
	if issues[0].Status != "open" {
		t.Fatalf("expected status %q, got %q", "open", issues[0].Status)
	}
	if issues[0].Title != "Ready task" {
		t.Fatalf("expected title %q, got %q", "Ready task", issues[0].Title)
	}
}

func TestReadyExcludeLabel(t *testing.T) {
	sup, dir := newTestProject(t)
	ctx := context.Background()

	if _, err := CreateTask(ctx, sup, dir, "Excluded task", "desc", CreateTaskOptions{
		Labels: []string{"skip-me"},
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	keepID, err := CreateTask(ctx, sup, dir, "Kept task", "desc", CreateTaskOptions{})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	issues, err := Ready(ctx, sup, dir, ReadyOptions{ExcludeLabels: []string{"skip-me"}})
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected exactly 1 ready issue after exclude-label filter, got %d: %+v", len(issues), issues)
	}
	if issues[0].ID != keepID {
		t.Fatalf("expected remaining issue id %q, got %q", keepID, issues[0].ID)
	}
}

func TestReadyExcludeType(t *testing.T) {
	sup, dir := newTestProject(t)
	ctx := context.Background()

	if _, err := CreateTask(ctx, sup, dir, "Epic", "desc", CreateTaskOptions{Type: "epic"}); err != nil {
		t.Fatalf("CreateTask (epic): %v", err)
	}
	taskID, err := CreateTask(ctx, sup, dir, "Plain task", "desc", CreateTaskOptions{Type: "task"})
	if err != nil {
		t.Fatalf("CreateTask (task): %v", err)
	}

	issues, err := Ready(ctx, sup, dir, ReadyOptions{ExcludeTypes: []string{"epic"}})
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected exactly 1 ready issue after exclude-type filter, got %d: %+v", len(issues), issues)
	}
	if issues[0].ID != taskID {
		t.Fatalf("expected remaining issue id %q, got %q", taskID, issues[0].ID)
	}
}

func TestReadyAssigneeFilterExcludesUnassigned(t *testing.T) {
	sup, dir := newTestProject(t)
	ctx := context.Background()

	if _, err := CreateTask(ctx, sup, dir, "Unassigned task", "desc", CreateTaskOptions{}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	issues, err := Ready(ctx, sup, dir, ReadyOptions{Assignee: "nobody-in-particular"})
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no ready issues for a non-matching assignee filter, got %d: %+v", len(issues), issues)
	}
}

func TestClaimReadyClaimsAndStopsReturningIssue(t *testing.T) {
	sup, dir := newTestProject(t)
	ctx := context.Background()

	taskID, err := CreateTask(ctx, sup, dir, "Claimable task", "desc", CreateTaskOptions{})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	claimed, err := ClaimReady(ctx, sup, dir, ReadyOptions{})
	if err != nil {
		t.Fatalf("ClaimReady: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("expected exactly 1 claimed issue, got %d: %+v", len(claimed), claimed)
	}
	if claimed[0].ID != taskID {
		t.Fatalf("expected claimed issue id %q, got %q", taskID, claimed[0].ID)
	}
	if claimed[0].Status != "in_progress" {
		t.Fatalf("expected status %q after claim, got %q", "in_progress", claimed[0].Status)
	}
	if claimed[0].Assignee == "" {
		t.Fatal("expected a non-empty assignee after claim")
	}

	// The task is now in_progress, so it must no longer show up as ready
	// (plain Ready) nor be claimable again (ClaimReady).
	stillReady, err := Ready(ctx, sup, dir, ReadyOptions{})
	if err != nil {
		t.Fatalf("Ready after claim: %v", err)
	}
	if len(stillReady) != 0 {
		t.Fatalf("expected no ready issues after claiming the only one, got %d: %+v", len(stillReady), stillReady)
	}

	reclaimed, err := ClaimReady(ctx, sup, dir, ReadyOptions{})
	if err != nil {
		t.Fatalf("ClaimReady (second attempt): %v", err)
	}
	if len(reclaimed) != 0 {
		t.Fatalf("expected no issue to be claimable a second time, got %d: %+v", len(reclaimed), reclaimed)
	}
}

func TestReopenIssueRequiresID(t *testing.T) {
	dir := t.TempDir()
	sup := tools.New(dir)
	if err := ReopenIssue(context.Background(), sup, dir, "", "some reason"); err == nil {
		t.Fatal("expected error for empty issueID")
	}
}

func TestReopenIssueReopensClosedIssue(t *testing.T) {
	sup, dir := newTestProject(t)
	ctx := context.Background()

	taskID, err := CreateTask(ctx, sup, dir, "Task to reopen", "desc", CreateTaskOptions{})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	closeRes, err := sup.Run(ctx, tools.Spec{Name: "bd", Args: []string{"close", taskID, "--json"}, Dir: dir})
	if err != nil || closeRes.ExitCode != 0 {
		t.Fatalf("bd close: err=%v exitCode=%d stderr=%s", err, closeRes.ExitCode, closeRes.Stderr)
	}

	if err := ReopenIssue(ctx, sup, dir, taskID, "Bagong found a blocking regression"); err != nil {
		t.Fatalf("ReopenIssue: %v", err)
	}

	issues, err := Ready(ctx, sup, dir, ReadyOptions{})
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(issues) != 1 || issues[0].ID != taskID {
		t.Fatalf("expected the reopened issue to be ready again, got %+v", issues)
	}
	if issues[0].Status != "open" {
		t.Fatalf("expected status %q after reopen, got %q", "open", issues[0].Status)
	}
}

func TestClaimReadyNoIssues(t *testing.T) {
	sup, dir := newTestProject(t)
	ctx := context.Background()

	claimed, err := ClaimReady(ctx, sup, dir, ReadyOptions{})
	if err != nil {
		t.Fatalf("ClaimReady: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("expected no claimed issues in an empty project, got %d", len(claimed))
	}
}
