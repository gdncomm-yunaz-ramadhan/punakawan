package mcpserver

import (
	"context"
	"os/exec"
	"testing"

	"github.com/ygrip/punakawan/internal/beads"
	"github.com/ygrip/punakawan/internal/tools"
)

func TestReopenTaskHandlerRequiresTaskID(t *testing.T) {
	a := newTestApp(t)

	if _, _, err := reopenTaskHandler(a)(context.Background(), nil, ReopenTaskInput{Reason: "why"}); err == nil {
		t.Fatal("expected an error when task_id is empty")
	}
}

func TestReopenTaskHandlerReopensIssue(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not installed")
	}
	a := newTestApp(t)
	ctx := context.Background()

	initRes, err := a.Supervisor.Run(ctx, tools.Spec{Name: "bd", Args: []string{"init", "--non-interactive", "--prefix", "test", "--skip-agents", "--skip-hooks", "-q"}, Dir: a.Workspace.Root})
	if err != nil || initRes.ExitCode != 0 {
		t.Fatalf("bd init: err=%v exitCode=%d stderr=%s", err, initRes.ExitCode, initRes.Stderr)
	}

	taskID, err := beads.CreateTask(ctx, a.Supervisor, a.Workspace.Root, "Task to reopen", "desc", beads.CreateTaskOptions{})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	closeRes, err := a.Supervisor.Run(ctx, tools.Spec{Name: "bd", Args: []string{"close", taskID, "--json"}, Dir: a.Workspace.Root})
	if err != nil || closeRes.ExitCode != 0 {
		t.Fatalf("bd close: err=%v exitCode=%d stderr=%s", err, closeRes.ExitCode, closeRes.Stderr)
	}

	_, out, err := reopenTaskHandler(a)(ctx, nil, ReopenTaskInput{TaskId: taskID, Reason: "Bagong found a blocking regression"})
	if err != nil {
		t.Fatalf("reopenTaskHandler: %v", err)
	}
	if !out.Reopened || out.TaskId != taskID {
		t.Fatalf("out = %+v, want Reopened=true TaskId=%q", out, taskID)
	}

	issues, err := beads.Ready(ctx, a.Supervisor, a.Workspace.Root, beads.ReadyOptions{})
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(issues) != 1 || issues[0].ID != taskID {
		t.Fatalf("expected the reopened issue to be ready again, got %+v", issues)
	}
}
