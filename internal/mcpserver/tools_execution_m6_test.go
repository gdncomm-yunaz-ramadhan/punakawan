package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// requireBd skips the test if bd is not installed, mirroring requireDolt.
func requireBd(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not installed")
	}
}

func initBdProject(t *testing.T, root string) {
	t.Helper()
	cmd := exec.Command("bd", "init", "--non-interactive", "--prefix", "test", "--skip-agents", "--skip-hooks", "-q")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init: %v\n%s", err, out)
	}
}

func seedRequirement(t *testing.T, a *app.App, id, title string) {
	t.Helper()
	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	err = store.Put(protocol.KnowledgeRecord{
		Id:     id,
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: "active",
		Title:  title,
		Source: protocol.KnowledgeRecordSource{
			Provider:    "manual",
			RetrievedAt: time.Now().UTC(),
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodManual,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State: protocol.KnowledgeRecordValidityStateObserved,
		},
	})
	if err != nil {
		t.Fatalf("seed requirement: %v", err)
	}
}

// TestSubmitTaskGraphAndListReadyTasks exercises the task-graph generator
// and ready-task selection tools end-to-end over the real MCP wire
// protocol: a requirement is seeded, submit_task_graph creates two Beads
// issues with a wired dependency, and list_ready_tasks confirms only the
// unblocked one is claimable.
func TestSubmitTaskGraphAndListReadyTasks(t *testing.T) {
	requireDolt(t)
	requireBd(t)

	a := newTestApp(t)
	initBdProject(t, a.Workspace.Root)
	seedRequirement(t, a, "pkw:req/smoke/REQ-1", "Refund approved order")

	cs := connect(t, a)

	var graphOut SubmitTaskGraphOutput
	callTool(t, cs, "submit_task_graph", map[string]any{
		"items": []map[string]any{
			{
				"local_key":           "migration",
				"requirement_id":      "pkw:req/smoke/REQ-1",
				"task_id":             "task-migration",
				"repository":          "repo-a",
				"scope":               "Create database migration",
				"acceptance_criteria": []string{"Migration applies cleanly"},
				"definition_of_done":  "Migration merged",
			},
			{
				"local_key":           "api",
				"requirement_id":      "pkw:req/smoke/REQ-1",
				"task_id":             "task-api",
				"repository":          "repo-a",
				"scope":               "Implement refund API",
				"acceptance_criteria": []string{"Refund endpoint returns 200"},
				"definition_of_done":  "API implemented and tested",
				"depends_on": []map[string]any{
					{"local_key": "migration", "type": "blocks"},
				},
			},
		},
	}, &graphOut)

	if len(graphOut.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(graphOut.Results))
	}

	var readyOut ReadyTasksOutput
	callTool(t, cs, "list_ready_tasks", map[string]any{}, &readyOut)

	// createTaskForRequirement stores the requirement's title as the bd
	// issue title and the task's scope as its description, so the two
	// tasks are distinguished by Description here, not Title.
	found := false
	for _, issue := range readyOut.Issues {
		if issue.Description == "Create database migration" {
			found = true
		}
		if issue.Description == "Implement refund API" {
			t.Fatalf("expected the api task to be blocked by migration, but it was listed as ready: %+v", issue)
		}
	}
	if !found {
		t.Fatalf("expected the migration task to be ready, got %+v", readyOut.Issues)
	}
}

// TestTaskExecutionLifecycle exercises the full per-task execution loop
// over the real MCP wire protocol: start_task_execution (after approving
// the worktree request directly, since granting approval is a human
// decision with no MCP tool of its own), write_file, check_diff,
// commit_task, and finish_task_execution.
func TestTaskExecutionLifecycle(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	const runID = "run-1"
	const taskID = "task-1"
	const repoID = "repo-a"

	if _, err := a.Worktrees.RequestApproval(runID, repoID, taskID, protocol.ApprovalRecordRequestedByPetruk); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := a.Worktrees.Approve(repoID, taskID, "test-human"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	var startOut StartTaskExecutionOutput
	callTool(t, cs, "start_task_execution", map[string]any{
		"run_id":       runID,
		"task_id":      taskID,
		"repo_id":      repoID,
		"requested_by": "petruk",
	}, &startOut)

	if info, err := os.Stat(startOut.WorktreePath); err != nil || !info.IsDir() {
		t.Fatalf("expected worktree to exist at %s: %v", startOut.WorktreePath, err)
	}

	var writeOut WriteFileOutput
	callTool(t, cs, "write_file", map[string]any{
		"repo_id": repoID,
		"task_id": taskID,
		"path":    "new_file.txt",
		"content": "hello from petruk\n",
	}, &writeOut)

	if _, err := os.Stat(filepath.Join(startOut.WorktreePath, "new_file.txt")); err != nil {
		t.Fatalf("expected new_file.txt to exist: %v", err)
	}

	var diffOut CheckDiffOutput
	callTool(t, cs, "check_diff", map[string]any{
		"run_id":  runID,
		"task_id": taskID,
		"repo_id": repoID,
	}, &diffOut)

	if !diffOut.Allowed {
		t.Fatalf("expected diff check to pass, violations: %v", diffOut.Violations)
	}

	commitArgs := map[string]any{
		"repo_id":      repoID,
		"task_id":      taskID,
		"message":      "add new_file.txt",
		"diff_allowed": diffOut.Allowed,
	}
	if len(diffOut.Violations) > 0 {
		commitArgs["violations"] = diffOut.Violations
	}
	var commitOut CommitTaskOutput
	callTool(t, cs, "commit_task", commitArgs, &commitOut)

	if commitOut.CommitSha == "" || commitOut.CommitSha == commitOut.BaseSha {
		t.Fatalf("expected a new commit SHA, got base=%q commit=%q", commitOut.BaseSha, commitOut.CommitSha)
	}

	var finishOut struct{}
	callTool(t, cs, "finish_task_execution", map[string]any{
		"run_id":  runID,
		"task_id": taskID,
		"repo_id": repoID,
		"status":  "committed",
	}, &finishOut)

	if _, err := os.Stat(startOut.WorktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree to be removed after finish, stat err = %v", err)
	}
}
