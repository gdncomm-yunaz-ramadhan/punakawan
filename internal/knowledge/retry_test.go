package knowledge

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestIsTransientConnErrRecognizesConnectionDeath(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"io.EOF", io.EOF, true},
		{"driver.ErrBadConn", driver.ErrBadConn, true},
		{"mysql.ErrInvalidConn", mysql.ErrInvalidConn, true},
		{"sql.ErrConnDone", sql.ErrConnDone, true},
		{"connection was closed message", errors.New("error running query: connection was closed"), true},
		{"broken pipe message", errors.New("write: broken pipe"), true},
		{"connection reset message", errors.New("read: connection reset by peer"), true},
		// A real data/query error - e.g. the caller's own "no rows" or a
		// constraint violation - must never be treated as retryable, or a
		// genuinely missing record would silently double the query cost for
		// no benefit.
		{"sql.ErrNoRows", sql.ErrNoRows, false},
		{"unrelated error", errors.New("knowledge: invalid record"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientConnErr(tc.err); got != tc.want {
				t.Fatalf("isTransientConnErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestWithConnRetryRetriesOnceOnTransientError guards the fix for a query
// that was genuinely in flight when the pooled connection died (e.g. the
// host slept for two hours, then "client connection went away while a
// query was executing"): the first attempt fails with a transient
// connection error, and a second attempt against what would be a freshly
// reopened connection succeeds.
func TestWithConnRetryRetriesOnceOnTransientError(t *testing.T) {
	calls := 0
	err := withConnRetry(func() error {
		calls++
		if calls == 1 {
			return io.EOF
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withConnRetry: %v, want nil after the retry succeeds", err)
	}
	if calls != 2 {
		t.Fatalf("read called %d times, want 2 (initial attempt + one retry)", calls)
	}
}

// TestWithConnRetryDoesNotRetryARealError guards against masking a genuine
// data/query error (e.g. sql.ErrNoRows, a bad query) behind a pointless
// second attempt that will only fail identically.
func TestWithConnRetryDoesNotRetryARealError(t *testing.T) {
	calls := 0
	wantErr := errors.New("knowledge: invalid record")
	err := withConnRetry(func() error {
		calls++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("withConnRetry err = %v, want %v", err, wantErr)
	}
	if calls != 1 {
		t.Fatalf("read called %d times, want 1 (no retry for a non-transient error)", calls)
	}
}

// TestWithConnRetryGivesUpAfterOneRetry guards that a connection which
// stays dead across the retry (a genuinely down server, not just a stale
// idle one) surfaces its error rather than looping - see withConnRetry's
// doc comment on why more than one retry buys nothing here.
func TestWithConnRetryGivesUpAfterOneRetry(t *testing.T) {
	calls := 0
	err := withConnRetry(func() error {
		calls++
		return io.EOF
	})
	if err != io.EOF {
		t.Fatalf("withConnRetry err = %v, want io.EOF", err)
	}
	if calls != 2 {
		t.Fatalf("read called %d times, want exactly 2 (no unbounded retry loop)", calls)
	}
}
