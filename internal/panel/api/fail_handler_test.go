package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func seedReviewWithStatus(t *testing.T, status protocol.ArtifactReviewMetadataStatus) (reviewID string, reviews *artifact.ReviewStore) {
	t.Helper()
	root := t.TempDir()
	reviews = &artifact.ReviewStore{WorkspaceRoot: root}
	reviewID = "review-1"
	if err := reviews.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: reviewID, Status: status},
		Artifact: protocol.ArtifactReviewArtifact{Type: protocol.ArtifactReviewArtifactTypePlan, Id: "plan-panel", Version: 1, RevisionHash: artifact.Hash([]byte("x"))},
		Review:   protocol.ArtifactReviewReview{Title: "t"},
	}); err != nil {
		t.Fatalf("PutReview: %v", err)
	}
	return reviewID, reviews
}

func failReview(t *testing.T, reviews *artifact.ReviewStore, reviewID string, reason string) *httptest.ResponseRecorder {
	t.Helper()
	var body *bytes.Reader
	if reason != "" {
		b, _ := json.Marshal(failReviewRequest{Reason: reason})
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/fail", body)
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	FailHandler(reviews, nil)(rec, req)
	return rec
}

func TestFailHandlerTransitionsEachInFlightStatusToFailed(t *testing.T) {
	inFlight := []protocol.ArtifactReviewMetadataStatus{
		protocol.ArtifactReviewMetadataStatusQueued,
		protocol.ArtifactReviewMetadataStatusRevising,
		protocol.ArtifactReviewMetadataStatusAwaitingClarification,
		protocol.ArtifactReviewMetadataStatusRevisionRequested,
	}
	for _, status := range inFlight {
		status := status
		t.Run(string(status), func(t *testing.T) {
			reviewID, reviews := seedReviewWithStatus(t, status)
			rec := failReview(t, reviews, reviewID, "agent crashed mid-revision")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
			}
			got, err := reviews.GetReview(reviewID)
			if err != nil {
				t.Fatalf("GetReview: %v", err)
			}
			if got.Metadata.Status != protocol.ArtifactReviewMetadataStatusFailed {
				t.Fatalf("Status = %q, want failed", got.Metadata.Status)
			}
			if got.Metadata.UpdatedAt == nil {
				t.Fatal("UpdatedAt not set after failing")
			}
		})
	}
}

func TestFailHandlerRejectsStatusesWithNoInFlightRun(t *testing.T) {
	notInFlight := []protocol.ArtifactReviewMetadataStatus{
		protocol.ArtifactReviewMetadataStatusDraft,
		protocol.ArtifactReviewMetadataStatusSubmitted,
		protocol.ArtifactReviewMetadataStatusProposalReady,
		protocol.ArtifactReviewMetadataStatusAccepted,
		protocol.ArtifactReviewMetadataStatusRejected,
		protocol.ArtifactReviewMetadataStatusCancelled,
		protocol.ArtifactReviewMetadataStatusConflicted,
	}
	for _, status := range notInFlight {
		status := status
		t.Run(string(status), func(t *testing.T) {
			reviewID, reviews := seedReviewWithStatus(t, status)
			rec := failReview(t, reviews, reviewID, "")
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body)
			}
			got, err := reviews.GetReview(reviewID)
			if err != nil {
				t.Fatalf("GetReview: %v", err)
			}
			if got.Metadata.Status != status {
				t.Fatalf("Status = %q, want unchanged %q after a rejected fail call", got.Metadata.Status, status)
			}
		})
	}
}

func TestFailHandlerIsIdempotentWhenAlreadyFailed(t *testing.T) {
	reviewID, reviews := seedReviewWithStatus(t, protocol.ArtifactReviewMetadataStatusFailed)
	rec := failReview(t, reviews, reviewID, "second report")
	if rec.Code != http.StatusOK {
		t.Fatalf("second fail status = %d, want 200 (idempotent): %s", rec.Code, rec.Body)
	}
	got, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if got.Metadata.Status != protocol.ArtifactReviewMetadataStatusFailed {
		t.Fatalf("Status = %q, want failed", got.Metadata.Status)
	}
}

func TestFailHandlerReturns404ForUnknownReview(t *testing.T) {
	reviews := &artifact.ReviewStore{WorkspaceRoot: t.TempDir()}
	rec := failReview(t, reviews, "no-such-review", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
