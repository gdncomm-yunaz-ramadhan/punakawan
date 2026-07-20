// Package execution orchestrates §11.1's ten-step per-task execution loop
// around the pieces built for it: gitops.WorktreeManager (steps 1-4 and 10),
// evidence.NewBundle/OpenJournal (step 8's home), and diffcheck/fileops/
// testrun/gitops.CommitTask (steps 5-9, invoked separately by the caller).
//
// Punakawan never calls an LLM itself (§28), so the actual edit (step 6) is
// performed by the external role through the file-editing tools, not by
// this package. Session only owns the surrounding lifecycle and the
// evidence/journal handles every other M6 tool writes into.
package execution

import (
	"context"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// Session is one task's execution state: an isolated worktree plus the
// evidence bundle and run journal every other M6 tool writes into.
type Session struct {
	RunID    string
	TaskID   string
	RepoID   string
	Worktree *gitops.Worktree
	Bundle   *evidence.Bundle
	Journal  *evidence.Journal
}

// StartTaskExecution performs §11.1 steps 1-4: it requires an already
// approved worktree-creation request (mgr.RequestApproval/Approve, exactly
// as gitops.WorktreeManager.Create itself requires), creates the isolated
// worktree and task branch, and opens this run's evidence bundle and
// journal. A "task-started" event is appended to the journal before
// returning.
func StartTaskExecution(ctx context.Context, mgr *gitops.WorktreeManager, workspaceRoot, repoPath, repoID, runID, taskID string) (*Session, error) {
	wt, err := mgr.Create(ctx, workspaceRoot, repoPath, repoID, taskID)
	if err != nil {
		return nil, fmt.Errorf("execution: create worktree: %w", err)
	}

	bundle, err := evidence.NewBundle(workspaceRoot, runID, taskID)
	if err != nil {
		return nil, fmt.Errorf("execution: create evidence bundle: %w", err)
	}

	journal, err := evidence.OpenJournal(workspaceRoot, runID)
	if err != nil {
		return nil, fmt.Errorf("execution: open journal: %w", err)
	}

	sess := &Session{
		RunID:    runID,
		TaskID:   taskID,
		RepoID:   repoID,
		Worktree: wt,
		Bundle:   bundle,
		Journal:  journal,
	}

	if err := sess.appendEvent(taskID, "task-started", protocol.EventResultSuccess, nil); err != nil {
		return nil, err
	}

	return sess, nil
}

// FinishTaskExecution performs §11.1 step 10: it appends a final journal
// event recording status ("committed" or "blocked", per the execution
// loop's own vocabulary in §11.3) and removes the isolated worktree.
func FinishTaskExecution(ctx context.Context, mgr *gitops.WorktreeManager, repoPath string, sess *Session, status string, payload map[string]any) error {
	result := protocol.EventResultSuccess
	if status != "committed" {
		result = protocol.EventResultFailure
	}
	if err := sess.appendEvent(sess.TaskID, "task-finished-"+status, result, payload); err != nil {
		return err
	}

	if err := mgr.Remove(ctx, repoPath, sess.Worktree); err != nil {
		return fmt.Errorf("execution: remove worktree: %w", err)
	}
	return nil
}

func (s *Session) appendEvent(taskID, operation string, result protocol.EventResult, payload map[string]any) error {
	event := protocol.Event{
		Id:        fmt.Sprintf("%s-%s-%d", s.RunID, taskID, time.Now().UnixNano()),
		Type:      "execution",
		Timestamp: time.Now().UTC(),
		RunId:     s.RunID,
		Task:      &taskID,
		Operation: operation,
		Result:    result,
	}
	if payload != nil {
		event.Payload = payload
	}
	if err := s.Journal.Append(event); err != nil {
		return fmt.Errorf("execution: append %s event: %w", operation, err)
	}
	return nil
}
