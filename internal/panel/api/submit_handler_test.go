package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/revision"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// stubDispatcher counts Dispatch calls and returns a deterministic run
// reference equal to the request id, mirroring what BDDispatcher would
// actually produce, without shelling out to bd.
type stubDispatcher struct {
	calls int
	err   error
}

func (d *stubDispatcher) Dispatch(ctx context.Context, req revision.Request) (revision.RunReference, error) {
	d.calls++
	if d.err != nil {
		return revision.RunReference{}, d.err
	}
	return revision.RunReference{RunID: req.RequestID, ParentTaskID: req.RequestID}, nil
}

func seedDraftReviewForSubmit(t *testing.T) (reviewID string, plans *artifact.PlanStore, reviews *artifact.ReviewStore) {
	t.Helper()
	root := t.TempDir()
	plans = &artifact.PlanStore{WorkspaceRoot: root}
	reviews = &artifact.ReviewStore{WorkspaceRoot: root}
	ref := seedPlan(t, plans, "plan-panel", commentTestPlanContent)
	reviewID = "review-1"
	if err := reviews.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: reviewID, Status: protocol.ArtifactReviewMetadataStatusDraft},
		Artifact: protocol.ArtifactReviewArtifact{Type: protocol.ArtifactReviewArtifactTypePlan, Id: "plan-panel", Version: ref.Version, RevisionHash: ref.RevisionHash},
		Review:   protocol.ArtifactReviewReview{Title: "Panel review"},
	}); err != nil {
		t.Fatalf("PutReview: %v", err)
	}
	return reviewID, plans, reviews
}

func submitReview(t *testing.T, reviews *artifact.ReviewStore, dispatcher revision.Dispatcher, reviewID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/submit", nil)
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	SubmitHandler(reviews, dispatcher)(rec, req)
	return rec
}

func TestSubmitHandlerFreezesAndDispatches(t *testing.T) {
	reviewID, _, reviews := seedDraftReviewForSubmit(t)
	dispatcher := &stubDispatcher{}

	rec := submitReview(t, reviews, dispatcher, reviewID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("dispatcher.calls = %d, want 1", dispatcher.calls)
	}

	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusQueued {
		t.Fatalf("Status = %q, want queued", review.Metadata.Status)
	}

	var resp submitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Run.RunID == "" || resp.Run.RunID != resp.RevisionRequest.Metadata.Id {
		t.Fatalf("resp = %+v, want run id to equal the revision request id", resp)
	}
}

func TestSubmitHandlerIsIdempotentOnRetry(t *testing.T) {
	reviewID, _, reviews := seedDraftReviewForSubmit(t)
	dispatcher := &stubDispatcher{}

	first := submitReview(t, reviews, dispatcher, reviewID)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201: %s", first.Code, first.Body)
	}
	second := submitReview(t, reviews, dispatcher, reviewID)
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want 200 (idempotent replay): %s", second.Code, second.Body)
	}
	if dispatcher.calls != 2 {
		t.Fatalf("dispatcher.calls = %d, want 2 (dispatch itself must also be idempotent)", dispatcher.calls)
	}

	var firstResp, secondResp submitResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstResp); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if firstResp.RevisionRequest.Metadata.Id != secondResp.RevisionRequest.Metadata.Id {
		t.Fatalf("request ids differ across retries: %q vs %q", firstResp.RevisionRequest.Metadata.Id, secondResp.RevisionRequest.Metadata.Id)
	}
}

func TestSubmitHandlerRejectsANonDraftReviewWithNoPendingSubmission(t *testing.T) {
	root := t.TempDir()
	reviews := &artifact.ReviewStore{WorkspaceRoot: root}
	if err := reviews.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: "review-1", Status: protocol.ArtifactReviewMetadataStatusCancelled},
		Artifact: protocol.ArtifactReviewArtifact{Type: protocol.ArtifactReviewArtifactTypePlan, Id: "plan-panel", Version: 1, RevisionHash: artifact.Hash([]byte("x"))},
		Review:   protocol.ArtifactReviewReview{Title: "t"},
	}); err != nil {
		t.Fatalf("PutReview: %v", err)
	}

	rec := submitReview(t, reviews, &stubDispatcher{}, "review-1")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body)
	}
}

func TestSubmitHandlerReturns404ForUnknownReview(t *testing.T) {
	reviews := &artifact.ReviewStore{WorkspaceRoot: t.TempDir()}
	rec := submitReview(t, reviews, &stubDispatcher{}, "no-such-review")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestCancelHandlerCancelsADraftReview(t *testing.T) {
	reviewID, _, reviews := seedDraftReviewForSubmit(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/cancel", nil)
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	CancelHandler(reviews)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusCancelled {
		t.Fatalf("Status = %q, want cancelled", review.Metadata.Status)
	}
}

func TestCancelHandlerIsIdempotentWhenAlreadyCancelled(t *testing.T) {
	reviewID, _, reviews := seedDraftReviewForSubmit(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/cancel", nil)
	req.SetPathValue("reviewId", reviewID)
	CancelHandler(reviews)(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/cancel", nil)
	req2.SetPathValue("reviewId", reviewID)
	rec2 := httptest.NewRecorder()
	CancelHandler(reviews)(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("second cancel status = %d, want 200 (idempotent)", rec2.Code)
	}
}

func TestCancelHandlerRejectsAnAlreadyAcceptedReview(t *testing.T) {
	root := t.TempDir()
	reviews := &artifact.ReviewStore{WorkspaceRoot: root}
	if err := reviews.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: "review-1", Status: protocol.ArtifactReviewMetadataStatusAccepted},
		Artifact: protocol.ArtifactReviewArtifact{Type: protocol.ArtifactReviewArtifactTypePlan, Id: "plan-panel", Version: 1, RevisionHash: artifact.Hash([]byte("x"))},
		Review:   protocol.ArtifactReviewReview{Title: "t"},
	}); err != nil {
		t.Fatalf("PutReview: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/review-1/cancel", nil)
	req.SetPathValue("reviewId", "review-1")
	rec := httptest.NewRecorder()
	CancelHandler(reviews)(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestTimelineHandlerReportsPendingSubmissionAfterSubmit(t *testing.T) {
	reviewID, _, reviews := seedDraftReviewForSubmit(t)
	if rec := submitReview(t, reviews, &stubDispatcher{}, reviewID); rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201: %s", rec.Code, rec.Body)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/"+reviewID+"/timeline", nil)
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	TimelineHandler(reviews)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var resp timelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RevisionRequest == nil || resp.Run == nil {
		t.Fatalf("resp = %+v, want a revision_request and run to be present after submit", resp)
	}
	if resp.Review.Metadata.Status != protocol.ArtifactReviewMetadataStatusQueued {
		t.Fatalf("Review.Status = %q, want queued", resp.Review.Metadata.Status)
	}
}

func TestTimelineHandlerHasNoRevisionRequestBeforeSubmit(t *testing.T) {
	reviewID, _, reviews := seedDraftReviewForSubmit(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/"+reviewID+"/timeline", nil)
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	TimelineHandler(reviews)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var resp timelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RevisionRequest != nil || resp.Run != nil {
		t.Fatalf("resp = %+v, want no revision_request/run before submit", resp)
	}
}

func TestTimelineHandlerReturns404ForUnknownReview(t *testing.T) {
	reviews := &artifact.ReviewStore{WorkspaceRoot: t.TempDir()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/no-such-review/timeline", nil)
	req.SetPathValue("reviewId", "no-such-review")
	rec := httptest.NewRecorder()
	TimelineHandler(reviews)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
