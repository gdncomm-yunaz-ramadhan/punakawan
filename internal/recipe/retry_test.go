package recipe

import (
	"context"
	"errors"
	"math/rand"
	"testing"
	"time"
)

// fakeFlakySearch fails with a retryable error the first failCount calls,
// then succeeds - a minimal stand-in for a real Jira HTTP client hitting a
// transient 429/5xx before recovering.
type fakeFlakySearch struct {
	failCount int
	err       error
	issues    []ResultRow
	calls     int
}

func (f *fakeFlakySearch) Search(ctx context.Context, jql, orderBy string, fields []string, maxResults int) ([]ResultRow, error) {
	f.calls++
	if f.calls <= f.failCount {
		return nil, f.err
	}
	return f.issues, nil
}

func fastTestPolicy() BackoffPolicy {
	return BackoffPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond, jitter: rand.New(rand.NewSource(1))}
}

func TestRetryingSearchRetriesTransientFailureAndSucceeds(t *testing.T) {
	inner := &fakeFlakySearch{failCount: 2, err: &RetryableError{Err: errors.New("429 too many requests")}, issues: []ResultRow{{Key: "TRF-1"}}}
	rs := &RetryingSearch{Client: inner, Policy: fastTestPolicy()}

	got, err := rs.Search(context.Background(), "project = TRF", "", nil, 20)
	if err != nil {
		t.Fatalf("Search: %v, want it to succeed after retrying", err)
	}
	if len(got) != 1 || got[0].Key != "TRF-1" {
		t.Fatalf("Search result = %+v, want [{TRF-1}]", got)
	}
	if inner.calls != 3 {
		t.Fatalf("inner.calls = %d, want 3 (2 failures + 1 success)", inner.calls)
	}
}

func TestRetryingSearchGivesUpAfterMaxAttempts(t *testing.T) {
	retryErr := &RetryableError{Err: errors.New("503 service unavailable")}
	inner := &fakeFlakySearch{failCount: 99, err: retryErr}
	rs := &RetryingSearch{Client: inner, Policy: fastTestPolicy()}

	_, err := rs.Search(context.Background(), "project = TRF", "", nil, 20)
	if err == nil {
		t.Fatal("Search: want an error once MaxAttempts is exhausted")
	}
	if inner.calls != 3 {
		t.Fatalf("inner.calls = %d, want exactly MaxAttempts=3", inner.calls)
	}
}

func TestRetryingSearchDoesNotRetryPermanentFailure(t *testing.T) {
	inner := &fakeFlakySearch{failCount: 99, err: errors.New("400 bad request: unknown field")}
	rs := &RetryingSearch{Client: inner, Policy: fastTestPolicy()}

	_, err := rs.Search(context.Background(), "project = TRF", "", nil, 20)
	if err == nil {
		t.Fatal("Search: want an error")
	}
	if inner.calls != 1 {
		t.Fatalf("inner.calls = %d, want exactly 1 (a non-retryable error must not be retried)", inner.calls)
	}
}

func TestRetryingSearchHonorsRetryAfter(t *testing.T) {
	inner := &fakeFlakySearch{failCount: 1, err: &RetryableError{Err: errors.New("429"), RetryAfter: 2 * time.Millisecond}, issues: []ResultRow{{Key: "TRF-1"}}}
	rs := &RetryingSearch{Client: inner, Policy: BackoffPolicy{MaxAttempts: 3, BaseDelay: time.Hour}}

	start := time.Now()
	if _, err := rs.Search(context.Background(), "project = TRF", "", nil, 20); err != nil {
		t.Fatalf("Search: %v", err)
	}
	// The huge BaseDelay would make this test hang for an hour if
	// RetryAfter weren't honored in place of the computed backoff.
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Search took %v, want it to honor the short RetryAfter instead of BaseDelay", elapsed)
	}
}

func TestRetryingSearchStopsOnContextCancellation(t *testing.T) {
	inner := &fakeFlakySearch{failCount: 99, err: &RetryableError{Err: errors.New("429")}}
	rs := &RetryingSearch{Client: inner, Policy: BackoffPolicy{MaxAttempts: 10, BaseDelay: 50 * time.Millisecond, MaxDelay: time.Second}}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := rs.Search(ctx, "project = TRF", "", nil, 20)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestIsRetryableRecognizesNetErrorShapedTemporary(t *testing.T) {
	if IsRetryable(nil) {
		t.Fatal("IsRetryable(nil) = true, want false")
	}
	if IsRetryable(errors.New("plain error")) {
		t.Fatal("IsRetryable(plain error) = true, want false")
	}
	if !IsRetryable(&RetryableError{Err: errors.New("x")}) {
		t.Fatal("IsRetryable(*RetryableError) = false, want true")
	}
	if !IsRetryable(fakeTemporary{}) {
		t.Fatal("IsRetryable(fakeTemporary) = false, want true for a Temporary()-shaped error")
	}
}

type fakeTemporary struct{}

func (fakeTemporary) Error() string   { return "temporary" }
func (fakeTemporary) Temporary() bool { return true }

func TestRetryingAgileRetriesTransientFailures(t *testing.T) {
	inner := &fakeFlakyAgile{failCount: 1, err: &RetryableError{Err: errors.New("429")}, boards: []Board{{ID: "1", Name: "Board"}}}
	ra := &RetryingAgile{Client: inner, Policy: fastTestPolicy()}

	got, err := ra.BoardsForProject(context.Background(), "TRF")
	if err != nil {
		t.Fatalf("BoardsForProject: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d boards, want 1", len(got))
	}
	if inner.calls != 2 {
		t.Fatalf("inner.calls = %d, want 2", inner.calls)
	}
}

type fakeFlakyAgile struct {
	failCount int
	err       error
	boards    []Board
	sprints   []Sprint
	calls     int
}

func (f *fakeFlakyAgile) BoardsForProject(ctx context.Context, projectKey string) ([]Board, error) {
	f.calls++
	if f.calls <= f.failCount {
		return nil, f.err
	}
	return f.boards, nil
}

func (f *fakeFlakyAgile) Sprints(ctx context.Context, boardID string) ([]Sprint, error) {
	f.calls++
	if f.calls <= f.failCount {
		return nil, f.err
	}
	return f.sprints, nil
}
