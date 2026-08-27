package delivery

import (
	"context"
	"errors"
	"testing"
)

func TestGitHubPRReviewPersistsApprovalAndResolution(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	findings := []map[string]any{{
		"title":       "Rounding loses cents",
		"explanation": "Use decimal rounding before persisting the refund.",
		"file":        "src/refund.go",
		"start_line":  12,
		"end_line":    13,
	}}

	proposed, err := s.ProposeGitHubPRReview(ctx, "propose-review", "acme/widgets", 42, "abc123", findings, "Please fix the rounding behavior.", "REQUEST_CHANGES", "execution-1")
	if err != nil {
		t.Fatalf("ProposeGitHubPRReview: %v", err)
	}
	if proposed.Status != "proposed" {
		t.Fatalf("proposed status = %q, want proposed", proposed.Status)
	}

	got, err := s.GetGitHubPRReview(ctx, proposed.ID)
	if err != nil {
		t.Fatalf("GetGitHubPRReview: %v", err)
	}
	if got.DeliveryExecutionID != "execution-1" || got.HeadSHA != "abc123" {
		t.Fatalf("reloaded review scope = %+v, want execution and SHA retained", got)
	}
	if len(got.Findings) != 1 || got.Findings[0]["file"] != "src/refund.go" {
		t.Fatalf("reloaded findings = %#v, want persisted finding", got.Findings)
	}

	if err := s.ApproveGitHubPRReview(ctx, "approve-review", proposed.ID); err != nil {
		t.Fatalf("ApproveGitHubPRReview: %v", err)
	}
	approved, err := s.GetGitHubPRReview(ctx, proposed.ID)
	if err != nil {
		t.Fatalf("GetGitHubPRReview after approval: %v", err)
	}
	if approved.Status != "approved" {
		t.Fatalf("approved status = %q, want approved", approved.Status)
	}

	submitted, err := s.ResolveGitHubPRReview(ctx, "submit-review", proposed.ID, "701", "")
	if err != nil {
		t.Fatalf("ResolveGitHubPRReview success: %v", err)
	}
	if submitted.Status != "submitted" || submitted.ExternalReviewID != "701" || submitted.Failure != "" {
		t.Fatalf("submitted review = %+v, want submitted review id 701 without failure", submitted)
	}
	if err := s.ApproveGitHubPRReview(ctx, "approve-submitted-review", proposed.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("ApproveGitHubPRReview after submission = %v, want ErrInvalidState", err)
	}

	failedProposal, err := s.ProposeGitHubPRReview(ctx, "propose-failed-review", "acme/widgets", 43, "def456", nil, "Cannot submit this review.", "COMMENT", "execution-1")
	if err != nil {
		t.Fatalf("ProposeGitHubPRReview for failure: %v", err)
	}
	if err := s.ApproveGitHubPRReview(ctx, "approve-failed-review", failedProposal.ID); err != nil {
		t.Fatalf("ApproveGitHubPRReview for failure: %v", err)
	}
	failed, err := s.ResolveGitHubPRReview(ctx, "resolve-failed-review", failedProposal.ID, "", "github unavailable")
	if err != nil {
		t.Fatalf("ResolveGitHubPRReview failure: %v", err)
	}
	if failed.Status != "failed" || failed.Failure != "github unavailable" || failed.ExternalReviewID != "" {
		t.Fatalf("failed review = %+v, want failed review with persisted failure", failed)
	}
}
