package mcpserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/pkg/protocol"
)

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

// TestTaskExecutionLifecycle exercises the full per-task execution loop
// over the real MCP wire protocol: start_task_execution (no approval
// required - creating a worktree is internal execution infrastructure),
// write_files, check_diff, commit_task, and finish_task_execution.
func TestTaskExecutionLifecycle(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	const runID = "run-1"
	const taskID = "task-1"
	const repoID = "repo-a"

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

	var writeOut WriteFilesOutput
	callTool(t, cs, "write_files", map[string]any{
		"repo_id": repoID,
		"task_id": taskID,
		"files": []map[string]any{
			{"path": "new_file.txt", "content": "hello from petruk\n"},
		},
	}, &writeOut)

	if len(writeOut.Results) != 1 || writeOut.Results[0].Error != "" {
		t.Fatalf("expected one clean write result, got %+v", writeOut.Results)
	}
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

	var evidenceOut ListTaskEvidenceOutput
	callTool(t, cs, "list_task_evidence", map[string]any{
		"run_id":  runID,
		"task_id": taskID,
	}, &evidenceOut)
	if len(evidenceOut.Records) != 1 || evidenceOut.Records[0].Type != protocol.EvidenceRecordTypeGitDiff {
		t.Fatalf("expected one git-diff evidence record after check_diff, got %+v", evidenceOut.Records)
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
