package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// TestTimelineHandlerRecoversAfterSimulatedPanelRestartMidRevision covers
// §18's "panel stops after submission" recovery path end to end: submit
// succeeds and the dispatcher is called (mirroring the moment right after
// a BD task graph was created), and then - simulating the panel process
// being killed before anything else happens, with no in-memory state
// surviving - a brand-new *artifact.ReviewStore pointed at the same
// on-disk WorkspaceRoot (standing in for a fresh process reading the same
// .punakawan/ directory after restart) must still report the exact same
// pending revision request and run reference via TimelineHandler, purely
// by recomputing the deterministic request id from the review's own
// on-disk state - there is no separate crash-recovery state to reconnect
// to, per submit_handler.go's TimelineHandler doc comment.
func TestTimelineHandlerRecoversAfterSimulatedPanelRestartMidRevision(t *testing.T) {
	reviewID, _, reviews := seedDraftReviewForSubmit(t)
	root := reviews.WorkspaceRoot

	dispatcher := &countingDispatcher{}
	rec := submitReview(t, reviews, dispatcher, reviewID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201: %s", rec.Code, rec.Body)
	}
	if dispatcher.count() != 1 {
		t.Fatalf("dispatcher.calls = %d, want 1", dispatcher.count())
	}

	// Simulate a full process restart: nothing about the *ReviewStore
	// value above (or any other in-memory state) survives: a brand-new
	// store is built pointed at the same on-disk workspace root, exactly
	// as internal/panel/server.New would do when the panel process is
	// started again.
	freshReviews := &artifact.ReviewStore{WorkspaceRoot: root}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/"+reviewID+"/timeline", nil)
	req.SetPathValue("reviewId", reviewID)
	timelineRec := httptest.NewRecorder()
	TimelineHandler(freshReviews)(timelineRec, req)

	if timelineRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", timelineRec.Code, timelineRec.Body)
	}
	var resp timelineResponse
	if err := json.Unmarshal(timelineRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RevisionRequest == nil || resp.Run == nil {
		t.Fatalf("resp = %+v, want the revision request and run to still be visible after a simulated restart", resp)
	}
	if resp.Review.Metadata.Status != protocol.ArtifactReviewMetadataStatusQueued {
		t.Fatalf("status = %q, want queued to survive the restart", resp.Review.Metadata.Status)
	}

	// A subsequent submit against the fresh store (as if the panel, now
	// restarted, retried the submission automatically or the user
	// clicked Submit again since the UI showed no confirmation) must
	// still resolve to the exact same request id and must not create a
	// second BD dispatch from scratch.
	freshDispatcher := &countingDispatcher{}
	retryRec := submitReview(t, freshReviews, freshDispatcher, reviewID)
	if retryRec.Code != http.StatusOK {
		t.Fatalf("post-restart resubmit status = %d, want 200 (idempotent replay): %s", retryRec.Code, retryRec.Body)
	}
	var retryResp submitResponse
	if err := json.Unmarshal(retryRec.Body.Bytes(), &retryResp); err != nil {
		t.Fatalf("decode retry: %v", err)
	}
	if retryResp.RevisionRequest.Metadata.Id != resp.RevisionRequest.Metadata.Id {
		t.Fatalf("post-restart request id = %q, want it to equal the pre-restart id %q", retryResp.RevisionRequest.Metadata.Id, resp.RevisionRequest.Metadata.Id)
	}
}

// TestTimelineHandlerRecoversWhileReviewIsRevisingOrAwaitingClarification
// extends the recovery test above to the intermediate lifecycle states a
// real revision run passes through (§10's "revising -> awaiting_clarification
// -> ... -> proposal_ready"), confirming TimelineHandler's recovery-by-
// recomputation works regardless of which of those states the review was
// last left in when the panel stopped - not just immediately after
// submit.
func TestTimelineHandlerRecoversWhileReviewIsRevisingOrAwaitingClarification(t *testing.T) {
	for _, status := range []protocol.ArtifactReviewMetadataStatus{
		protocol.ArtifactReviewMetadataStatusRevising,
		protocol.ArtifactReviewMetadataStatusAwaitingClarification,
	} {
		t.Run(string(status), func(t *testing.T) {
			reviewID, _, reviews := seedDraftReviewForSubmit(t)
			root := reviews.WorkspaceRoot

			dispatcher := &countingDispatcher{}
			if rec := submitReview(t, reviews, dispatcher, reviewID); rec.Code != http.StatusCreated {
				t.Fatalf("submit status = %d, want 201: %s", rec.Code, rec.Body)
			}

			// An agent (or the panel, observing the run) would have
			// advanced the review's status past "queued" as the run
			// progresses - simulate that external state transition
			// directly, since driving it via a real dispatcher/agent is
			// out of this package's scope.
			review, err := reviews.GetReview(reviewID)
			if err != nil {
				t.Fatalf("GetReview: %v", err)
			}
			review.Metadata.Status = status
			if err := reviews.PutReview(review); err != nil {
				t.Fatalf("PutReview: %v", err)
			}

			// Simulated restart: fresh store, same workspace root.
			freshReviews := &artifact.ReviewStore{WorkspaceRoot: root}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/"+reviewID+"/timeline", nil)
			req.SetPathValue("reviewId", reviewID)
			rec := httptest.NewRecorder()
			TimelineHandler(freshReviews)(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
			}
			var resp timelineResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Review.Metadata.Status != status {
				t.Fatalf("status = %q, want %q to survive the restart", resp.Review.Metadata.Status, status)
			}
			if resp.RevisionRequest == nil || resp.Run == nil {
				t.Fatalf("resp = %+v, want the revision request/run still visible while %s", resp, status)
			}
		})
	}
}
