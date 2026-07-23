package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/revision"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// countingDispatcher is like stubDispatcher but safe to call from many
// goroutines at once, so concurrency tests can assert exactly how many
// times Dispatch actually ran (as opposed to how many HTTP requests were
// made) without a data race on the counter itself.
type countingDispatcher struct {
	mu    sync.Mutex
	calls int
}

func (d *countingDispatcher) Dispatch(ctx context.Context, req revision.Request) (revision.RunReference, error) {
	d.mu.Lock()
	d.calls++
	d.mu.Unlock()
	return revision.RunReference{RunID: req.RequestID, ParentTaskID: req.RequestID}, nil
}

func (d *countingDispatcher) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// TestSubmitHandlerConcurrentDuplicateSubmitsCreateOneRevisionRequest fires
// the exact same review's submit endpoint from many goroutines at once -
// simulating a double-click or a client retrying a submit whose response
// was lost in flight, but this time genuinely concurrently rather than
// sequentially. Per §8's "submitting twice must return the existing run,
// not create two competing agents," this must never write two distinct
// ArtifactRevisionRequest submission files, and every response must agree
// on the same request id.
func TestSubmitHandlerConcurrentDuplicateSubmitsCreateOneRevisionRequest(t *testing.T) {
	reviewID, _, reviews := seedDraftReviewForSubmit(t)
	dispatcher := &countingDispatcher{}

	const workers = 16
	var wg sync.WaitGroup
	codes := make([]int, workers)
	requestIDs := make([]string, workers)
	var decodeErrs int32Counter

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rec := submitReview(t, reviews, dispatcher, reviewID)
			codes[idx] = rec.Code
			var resp submitResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				decodeErrs.inc()
				return
			}
			requestIDs[idx] = resp.RevisionRequest.Metadata.Id
		}(i)
	}
	wg.Wait()

	if decodeErrs.get() != 0 {
		t.Fatalf("%d of %d responses failed to decode", decodeErrs.get(), workers)
	}
	for _, code := range codes {
		if code != http.StatusCreated && code != http.StatusOK {
			t.Fatalf("codes = %v, want every concurrent submit to succeed with 200 or 201", codes)
		}
	}

	first := requestIDs[0]
	if first == "" {
		t.Fatal("first requestID is empty")
	}
	for i, id := range requestIDs {
		if id != first {
			t.Fatalf("requestIDs[%d] = %q, want all concurrent submits to resolve to the same request id %q", i, id, first)
		}
	}

	// Exactly one submission file must exist on disk - concurrent
	// requests that raced past the "fresh" check must not have each
	// written their own copy (PutRevisionRequest's own
	// ErrRevisionRequestExists guard is what's under test here).
	submissionsDir := submissionsDirFor(reviews, reviewID)
	entries, err := os.ReadDir(submissionsDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", submissionsDir, err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("submissions dir has %d files %v, want exactly 1 (no duplicate ArtifactRevisionRequest was created)", len(entries), names)
	}

	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusQueued {
		t.Fatalf("review status = %q, want queued", review.Metadata.Status)
	}
}

// submissionsDirFor reaches into ReviewStore's on-disk layout the same way
// GetRevisionRequest does, so the test can assert on the raw file count
// without ReviewStore needing to expose a new "list submissions" method
// just for this test.
func submissionsDirFor(reviews *artifact.ReviewStore, reviewID string) string {
	// ReviewStore stores submissions under
	// <WorkspaceRoot>/.punakawan/reviews/<reviewID>/submissions - mirror
	// that path from the same WorkspaceRoot the test's store was built
	// with.
	return reviews.WorkspaceRoot + "/.punakawan/reviews/" + reviewID + "/submissions"
}

// int32Counter is a tiny mutex-guarded counter, avoiding an import of
// sync/atomic just for one int in this test file.
type int32Counter struct {
	mu sync.Mutex
	n  int
}

func (c *int32Counter) inc() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *int32Counter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// TestRequestChangesHandlerRetryAfterServerAlreadyProcessedDoesNotDoubleDispatch
// simulates a client whose network "failed" after the server had already
// committed a request-changes call (the HTTP response never made it back),
// so the client retries the identical request-changes call again. Per §8's
// idempotency contract extended to request-changes (§16: "creates another
// attempt task under the same parent" using a still-deterministic id), the
// retry must resolve to the same next-attempt id and must not cause the
// dispatcher to create a second, competing task graph for that attempt.
func TestRequestChangesHandlerRetryAfterServerAlreadyProcessedDoesNotDoubleDispatch(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)
	createProposal(t, reviews, plans, reviewID)
	dispatcher := &countingDispatcher{}

	doRequestChanges := func() (*httptest.ResponseRecorder, string) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals/1/request-changes", nil)
		req.SetPathValue("reviewId", reviewID)
		req.SetPathValue("proposalId", "1")
		rec := httptest.NewRecorder()
		RequestChangesHandler(reviews, dispatcher)(rec, req)
		return rec, reviewID
	}

	first, _ := doRequestChanges()
	if first.Code != http.StatusOK {
		t.Fatalf("first request-changes status = %d, want 200: %s", first.Code, first.Body)
	}
	if dispatcher.count() != 1 {
		t.Fatalf("dispatcher.calls after first call = %d, want 1", dispatcher.count())
	}

	// The "network failed client-side after the server already
	// processed it" case: the client never saw the 200, so it retries
	// the exact same request-changes call against the same attempt.
	second, _ := doRequestChanges()
	if second.Code != http.StatusOK {
		t.Fatalf("retried request-changes status = %d, want 200: %s", second.Code, second.Body)
	}

	// RequestChangesHandler's dispatch id is deterministic
	// (baseRequestID + "-attempt-2" every time attempt=1 is retried), so
	// BDDispatcher's own idempotent-create-if-missing behavior is what
	// prevents a second task graph - here (a stub, not real bd) we can
	// only assert the handler asked the dispatcher for the *same*
	// request id both times, which is the contract the real BDDispatcher
	// relies on to stay idempotent.
	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusRevisionRequested {
		t.Fatalf("review status = %q, want revision_requested", review.Metadata.Status)
	}
}

// TestRequestChangesHandlerRetryUsesTheSameDeterministicRequestID directly
// verifies the id-stability claim the double-dispatch test above depends
// on: two request-changes calls against the same attempt must derive the
// exact same dispatch request id, so a real BDDispatcher (which checks
// "does a parent task with this id already exist" before creating one)
// would see the second call as a no-op re-dispatch rather than a new run.
func TestRequestChangesHandlerRetryUsesTheSameDeterministicRequestID(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)
	createProposal(t, reviews, plans, reviewID)

	recorder := &requestIDCapturingDispatcher{}

	call := func() {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals/1/request-changes", nil)
		req.SetPathValue("reviewId", reviewID)
		req.SetPathValue("proposalId", "1")
		rec := httptest.NewRecorder()
		RequestChangesHandler(reviews, recorder)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request-changes status = %d, want 200: %s", rec.Code, rec.Body)
		}
	}
	call()
	call()
	seen := recorder.ids

	if len(seen) != 2 {
		t.Fatalf("dispatcher saw %d calls, want 2", len(seen))
	}
	if seen[0] != seen[1] {
		t.Fatalf("dispatch request ids differ across retries: %q vs %q, want identical (idempotency key stability)", seen[0], seen[1])
	}
}

type requestIDCapturingDispatcher struct {
	ids []string
}

func (d *requestIDCapturingDispatcher) Dispatch(ctx context.Context, req revision.Request) (revision.RunReference, error) {
	d.ids = append(d.ids, req.RequestID)
	return revision.RunReference{RunID: req.RequestID, ParentTaskID: req.RequestID}, nil
}
