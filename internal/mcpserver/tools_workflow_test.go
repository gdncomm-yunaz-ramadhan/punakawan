package mcpserver

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/internal/app"
)

func newTestRun(t *testing.T, a *app.App, runID string) {
	t.Helper()
	if _, _, err := createWorkflowRunHandler(a)(context.Background(), nil, CreateWorkflowRunInput{
		RunId:        runID,
		WorkflowName: "implementation-only",
	}); err != nil {
		t.Fatalf("create_workflow_run: %v", err)
	}
}

// advanceRunToReviewing drives runID through every intermediate state
// internal/workflow.Advance now requires (punokawan-e8x) before a caller
// can request "completed", so tests exercising completion-time behavior
// don't also have to exercise the state-sequence enforcement itself.
func advanceRunToReviewing(t *testing.T, a *app.App, runID string) {
	t.Helper()
	for _, next := range []string{"context-building", "planning", "awaiting-approval", "executing", "reviewing"} {
		if _, _, err := advanceWorkflowHandler(a)(context.Background(), nil, AdvanceWorkflowInput{RunId: runID, NextState: next}); err != nil {
			t.Fatalf("advance_workflow to %s: %v", next, err)
		}
	}
}

func TestAdvanceWorkflowAllowsCompletionWithNoBagongReview(t *testing.T) {
	a := newTestApp(t)
	newTestRun(t, a, "run-1")
	advanceRunToReviewing(t, a, "run-1")

	if _, _, err := advanceWorkflowHandler(a)(context.Background(), nil, AdvanceWorkflowInput{RunId: "run-1", NextState: "completed"}); err != nil {
		t.Fatalf("advance_workflow: %v", err)
	}
}
