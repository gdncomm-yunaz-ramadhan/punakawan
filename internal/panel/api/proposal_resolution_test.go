package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

const resolutionBasePlanContent = "# Plan\n\n## Security\n\nBinds to loopback only.\n"
const resolutionRevisedPlanContent = "# Plan\n\n## Security\n\nBinds to loopback only by default, with an optional authenticated LAN mode.\n"

// seedReviewWithProposal builds a plan, a queued review over it, and one
// stored proposal attempt against that review - the state Accept/Reject
// (driven by the Context Improvements inbox, not the deleted human-review
// create-proposal flow) always start from. It writes the proposal directly
// through ReviewStore.PutProposal rather than via an HTTP handler, since
// proposal creation for a learning proposal happens programmatically, not
// through a user-facing endpoint.
func seedReviewWithProposal(t *testing.T) (reviewID string, plans *artifact.PlanStore, reviews *artifact.ReviewStore) {
	t.Helper()
	root := t.TempDir()
	plans = &artifact.PlanStore{WorkspaceRoot: root}
	reviews = &artifact.ReviewStore{WorkspaceRoot: root}

	base, err := plans.CreateVersion("plan-panel", "punakawan", []byte(resolutionBasePlanContent), time.Now())
	if err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	reviewID = "review-1"
	if err := reviews.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: reviewID, WorkspaceId: "punakawan", Status: protocol.ArtifactReviewMetadataStatusQueued},
		Artifact: protocol.ArtifactReviewArtifact{Type: protocol.ArtifactReviewArtifactTypePlan, Id: "plan-panel", Version: base.Version, RevisionHash: base.RevisionHash},
		Review:   protocol.ArtifactReviewReview{Title: "Add LAN mode"},
	}); err != nil {
		t.Fatalf("PutReview: %v", err)
	}

	if err := reviews.PutProposal(protocol.ArtifactRevisionProposal{
		Metadata: protocol.ArtifactRevisionProposalMetadata{Attempt: 1, Id: "proposal-1", ReviewId: reviewID},
		Base:     protocol.ArtifactRevisionProposalBase{ArtifactId: "plan-panel", RevisionHash: base.RevisionHash, Version: base.Version},
	}, []byte(resolutionRevisedPlanContent), nil); err != nil {
		t.Fatalf("PutProposal: %v", err)
	}
	return reviewID, plans, reviews
}

func TestAcceptProposalHandlerCreatesANewCanonicalVersion(t *testing.T) {
	reviewID, plans, reviews := seedReviewWithProposal(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals/1/accept", nil)
	req.SetPathValue("reviewId", reviewID)
	req.SetPathValue("proposalId", "1")
	rec := httptest.NewRecorder()
	AcceptProposalHandler(reviews, ArtifactStores{Plans: plans})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	current, err := plans.Current("plan-panel")
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current.Version != 2 {
		t.Fatalf("current.Version = %d, want 2 (accepted proposal became the new canonical version)", current.Version)
	}
	content, _, err := plans.Version("plan-panel", 2)
	if err != nil {
		t.Fatalf("Version(2): %v", err)
	}
	if string(content) != resolutionRevisedPlanContent {
		t.Fatalf("content = %q, want the accepted proposal's content", content)
	}

	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusAccepted {
		t.Fatalf("review status = %q, want accepted", review.Metadata.Status)
	}
}

func TestAcceptProposalHandlerDetectsConflict(t *testing.T) {
	reviewID, plans, reviews := seedReviewWithProposal(t)

	// The canonical plan changes out from under the review after the
	// proposal was generated - acceptance must refuse, not silently
	// overwrite the newer version.
	if _, err := plans.CreateVersion("plan-panel", "punakawan", []byte("# Plan\n\nSomeone else changed this.\n"), time.Now()); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals/1/accept", nil)
	req.SetPathValue("reviewId", reviewID)
	req.SetPathValue("proposalId", "1")
	rec := httptest.NewRecorder()
	AcceptProposalHandler(reviews, ArtifactStores{Plans: plans})(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusConflicted {
		t.Fatalf("review status = %q, want conflicted", review.Metadata.Status)
	}
}

func TestAcceptProposalHandlerRefusesAFailedValidation(t *testing.T) {
	root := t.TempDir()
	plans := &artifact.PlanStore{WorkspaceRoot: root}
	reviews := &artifact.ReviewStore{WorkspaceRoot: root}
	base, err := plans.CreateVersion("plan-panel", "punakawan", []byte(resolutionBasePlanContent), time.Now())
	if err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	reviewID := "review-1"
	if err := reviews.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: reviewID, WorkspaceId: "punakawan", Status: protocol.ArtifactReviewMetadataStatusQueued},
		Artifact: protocol.ArtifactReviewArtifact{Type: protocol.ArtifactReviewArtifactTypePlan, Id: "plan-panel", Version: base.Version, RevisionHash: base.RevisionHash},
		Review:   protocol.ArtifactReviewReview{Title: "Add LAN mode"},
	}); err != nil {
		t.Fatalf("PutReview: %v", err)
	}
	failed := protocol.ArtifactRevisionProposalResultsValidationStatusFailed
	if err := reviews.PutProposal(protocol.ArtifactRevisionProposal{
		Metadata: protocol.ArtifactRevisionProposalMetadata{Attempt: 1, Id: "proposal-1", ReviewId: reviewID},
		Base:     protocol.ArtifactRevisionProposalBase{ArtifactId: "plan-panel", RevisionHash: base.RevisionHash, Version: base.Version},
		Results:  &protocol.ArtifactRevisionProposalResults{ValidationStatus: &failed},
	}, []byte(resolutionRevisedPlanContent), nil); err != nil {
		t.Fatalf("PutProposal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals/1/accept", nil)
	req.SetPathValue("reviewId", reviewID)
	req.SetPathValue("proposalId", "1")
	rec := httptest.NewRecorder()
	AcceptProposalHandler(reviews, ArtifactStores{Plans: plans})(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestRejectProposalHandlerNeverTouchesCanonicalContent(t *testing.T) {
	reviewID, plans, reviews := seedReviewWithProposal(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals/1/reject", nil)
	req.SetPathValue("reviewId", reviewID)
	req.SetPathValue("proposalId", "1")
	rec := httptest.NewRecorder()
	RejectProposalHandler(reviews)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	current, err := plans.Current("plan-panel")
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current.Version != 1 {
		t.Fatalf("current.Version = %d, want 1 (rejection must not create a new version)", current.Version)
	}
	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusRejected {
		t.Fatalf("review status = %q, want rejected", review.Metadata.Status)
	}
}
