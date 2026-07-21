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

func TestAdvanceWorkflowRefusesCompletionWithBlockingBagongFindings(t *testing.T) {
	a := newTestApp(t)
	newTestRun(t, a, "run-1")

	verdict := "reject"
	summary := "found a regression"
	if _, _, err := submitBagongReviewHandler(a)(context.Background(), nil, SubmitBagongReviewInput{
		RunId: "run-1",
		Title: "Bagong review",
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

	verdict := "approve"
	summary := "looks correct, no blocking issues"
	if _, _, err := submitBagongReviewHandler(a)(context.Background(), nil, SubmitBagongReviewInput{
		RunId: "run-1",
		Title: "Bagong review",
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

	if _, _, err := advanceWorkflowHandler(a)(context.Background(), nil, AdvanceWorkflowInput{RunId: "run-1", NextState: "completed"}); err != nil {
		t.Fatalf("advance_workflow: %v", err)
	}
}
