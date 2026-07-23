package recipe

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"
)

// RetryableError marks an error from a JiraSearchClient/JiraAgileClient
// implementation as transient (a 429 rate limit, a 5xx, or a network
// timeout) rather than a permanent failure (400, 404, malformed JQL). No
// concrete Jira HTTP client exists on the Go side yet (JiraSearchClient
// and JiraAgileClient are still honest gaps - see their doc comments in
// compiler.go/validation.go), so this package cannot itself inspect an
// HTTP status code; whatever client eventually implements those
// interfaces is expected to wrap a transient failure in RetryableError
// (or satisfy the Temporary()-shaped check IsRetryable also recognizes)
// so RetryingSearch/RetryingAgile know to back off and retry instead of
// giving up on the first blip - task q9r.7 #2's "rate-limit and
// transient-failure handling".
type RetryableError struct {
	Err error
	// RetryAfter is an optional server-supplied delay (e.g. a 429's
	// Retry-After header) that, when set, is honored instead of the
	// backoff schedule's own computed delay for that attempt.
	RetryAfter time.Duration
}

func (e *RetryableError) Error() string { return e.Err.Error() }
func (e *RetryableError) Unwrap() error { return e.Err }

// temporary is the same duck-typed interface net.Error and similar
// standard-library errors satisfy, so a plain network timeout is
// recognized as retryable without every caller needing to wrap it in
// RetryableError explicitly.
type temporary interface {
	Temporary() bool
}

// IsRetryable reports whether err (as returned by a JiraSearchClient or
// JiraAgileClient call) should be retried rather than failed outright.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var re *RetryableError
	if errors.As(err, &re) {
		return true
	}
	var t temporary
	if errors.As(err, &t) {
		return t.Temporary()
	}
	return false
}

// retryAfter extracts RetryableError's optional server-supplied delay, or
// zero if err carries none (or isn't a RetryableError at all).
func retryAfter(err error) time.Duration {
	var re *RetryableError
	if errors.As(err, &re) {
		return re.RetryAfter
	}
	return 0
}

// BackoffPolicy configures RetryingSearch/RetryingAgile's retry schedule.
// The zero value is invalid; use NewBackoffPolicy for sane defaults.
type BackoffPolicy struct {
	// MaxAttempts is the total number of tries, including the first
	// (non-retry) attempt. MaxAttempts<=1 disables retrying entirely.
	MaxAttempts int
	// BaseDelay is the delay before the second attempt; each subsequent
	// attempt doubles it (full jitter applied), capped at MaxDelay.
	BaseDelay time.Duration
	MaxDelay  time.Duration
	// jitter is a seeded source for tests to make backoff deterministic;
	// nil (the zero value) falls back to math/rand's global source.
	jitter *rand.Rand
}

// DefaultBackoffPolicy is a conservative default: 3 attempts total (1
// original + 2 retries), starting at 250ms and capping at 4s - enough to
// ride out a single rate-limit window or a transient network blip without
// making a caller wait long for what is ultimately a non-transient
// failure.
func DefaultBackoffPolicy() BackoffPolicy {
	return BackoffPolicy{MaxAttempts: 3, BaseDelay: 250 * time.Millisecond, MaxDelay: 4 * time.Second}
}

func (p BackoffPolicy) delay(attempt int, err error) time.Duration {
	if wait := retryAfter(err); wait > 0 {
		return wait
	}
	d := p.BaseDelay << uint(attempt-1)
	if p.MaxDelay > 0 && d > p.MaxDelay {
		d = p.MaxDelay
	}
	if d <= 0 {
		return 0
	}
	// Full jitter (0..d) to avoid every concurrent caller retrying in
	// lockstep against the same rate-limited instance.
	if p.jitter != nil {
		return time.Duration(p.jitter.Int63n(int64(d) + 1))
	}
	return time.Duration(rand.Int63n(int64(d) + 1))
}

// withRetry runs op up to policy.MaxAttempts times, sleeping between
// attempts per policy.delay, stopping early on a non-retryable error, a
// success, or ctx cancellation - shared by RetryingSearch.Search and
// RetryingAgile.BoardsForProject/Sprints so both interfaces get identical
// behavior from one implementation.
func withRetry[T any](ctx context.Context, policy BackoffPolicy, op func() (T, error)) (T, error) {
	maxAttempts := policy.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var zero T
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := op()
		if err == nil {
			return result, nil
		}
		lastErr = err
		if attempt == maxAttempts || !IsRetryable(err) {
			return zero, lastErr
		}

		select {
		case <-time.After(policy.delay(attempt, err)):
		case <-ctx.Done():
			return zero, fmt.Errorf("recipe: retry: %w (last error before cancellation: %v)", ctx.Err(), lastErr)
		}
	}
	return zero, lastErr
}

// RetryingSearch wraps a JiraSearchClient with BackoffPolicy's retry
// schedule, applied to transient failures only (§2's "rate-limit and
// transient-failure handling"). A permanent failure (a malformed JQL
// clause, an unknown field) is returned on the first attempt exactly as
// today, since retrying it would only delay an inevitable failure.
type RetryingSearch struct {
	Client JiraSearchClient
	Policy BackoffPolicy
}

// NewRetryingSearch wraps client with DefaultBackoffPolicy.
func NewRetryingSearch(client JiraSearchClient) *RetryingSearch {
	return &RetryingSearch{Client: client, Policy: DefaultBackoffPolicy()}
}

func (r *RetryingSearch) Search(ctx context.Context, jql, orderBy string, fields []string, maxResults int) ([]JiraIssue, error) {
	return withRetry(ctx, r.Policy, func() ([]JiraIssue, error) {
		return r.Client.Search(ctx, jql, orderBy, fields, maxResults)
	})
}

// RetryingAgile wraps a JiraAgileClient with the same retry schedule as
// RetryingSearch, for the board/sprint lookups the built-in resolvers
// depend on.
type RetryingAgile struct {
	Client JiraAgileClient
	Policy BackoffPolicy
}

// NewRetryingAgile wraps client with DefaultBackoffPolicy.
func NewRetryingAgile(client JiraAgileClient) *RetryingAgile {
	return &RetryingAgile{Client: client, Policy: DefaultBackoffPolicy()}
}

func (r *RetryingAgile) BoardsForProject(ctx context.Context, projectKey string) ([]Board, error) {
	return withRetry(ctx, r.Policy, func() ([]Board, error) {
		return r.Client.BoardsForProject(ctx, projectKey)
	})
}

func (r *RetryingAgile) Sprints(ctx context.Context, boardID string) ([]Sprint, error) {
	return withRetry(ctx, r.Policy, func() ([]Sprint, error) {
		return r.Client.Sprints(ctx, boardID)
	})
}
