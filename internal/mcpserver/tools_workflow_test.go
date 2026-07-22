package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/pkg/protocol"
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

// bagongCapsuleID issues a bagong-role capsule for taskID via
// requestCapsuleHandler, for tests exercising submit_bagong_review's now-
// required capsule_id precondition (punokawan-ow9).
func bagongCapsuleID(t *testing.T, a *app.App, taskID string) string {
	t.Helper()
	_, c, err := requestCapsuleHandler(a)(context.Background(), nil, RequestCapsuleInput{
		TaskId:    taskID,
		Role:      "bagong",
		Objective: "test objective",
	})
	if err != nil {
		t.Fatalf("request_capsule: %v", err)
	}
	return c.Id
}

func TestAdvanceWorkflowRefusesCompletionWithBlockingBagongFindings(t *testing.T) {
	a := newTestApp(t)
	newTestRun(t, a, "run-1")
	advanceRunToReviewing(t, a, "run-1")
	capsuleID := bagongCapsuleID(t, a, "run-1")

	verdict := "reject"
	summary := "found a regression"
	if _, _, err := submitBagongReviewHandler(a)(context.Background(), nil, SubmitBagongReviewInput{
		RunId:     "run-1",
		CapsuleId: capsuleID,
		Title:     "Bagong review",
		Review: protocol.KnowledgeRecordBagongReview{
			Verdict:          &verdict,
			HonestSummary:    &summary,
			BlockingFindings: []string{"checkout total is off by one cent on discount codes"},
		},
	}); err != nil {
		t.Fatalf("submit_bagong_review: %v", err)
	}

	_, _, err := advanceWorkflowHandler(a)(context.Background(), nil, AdvanceWorkflowInput{RunId: "run-1", NextState: "completed"})
	if err == nil {
		t.Fatal("expected an error completing a run with unresolved blocking Bagong findings")
	}
	if !strings.Contains(err.Error(), "off by one cent") || !strings.Contains(err.Error(), "reopen_task") {
		t.Errorf("error = %q, want it to name the finding and point at reopen_task", err.Error())
	}
}

func TestAdvanceWorkflowAllowsCompletionWithCleanBagongReview(t *testing.T) {
	a := newTestApp(t)
	newTestRun(t, a, "run-1")
	advanceRunToReviewing(t, a, "run-1")
	capsuleID := bagongCapsuleID(t, a, "run-1")

	verdict := "approve"
	summary := "looks correct, no blocking issues"
	if _, _, err := submitBagongReviewHandler(a)(context.Background(), nil, SubmitBagongReviewInput{
		RunId:     "run-1",
		CapsuleId: capsuleID,
		Title:     "Bagong review",
		Review: protocol.KnowledgeRecordBagongReview{
			Verdict:       &verdict,
			HonestSummary: &summary,
		},
	}); err != nil {
		t.Fatalf("submit_bagong_review: %v", err)
	}

	if _, _, err := advanceWorkflowHandler(a)(context.Background(), nil, AdvanceWorkflowInput{RunId: "run-1", NextState: "completed"}); err != nil {
		t.Fatalf("advance_workflow: %v", err)
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
