package knowledge

import (
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
)

// mysqlDeadlockOrSerializationFailure is MySQL/Dolt error 1213 (SQLSTATE
// 40001): "Deadlock found when trying to get lock" / Dolt's own
// "serialization failure: this transaction conflicts with a committed
// transaction from another client, try restarting transaction." Both
// report as this same error number and are, by definition, transient -
// the standard advice for both is exactly "retry the transaction," which
// Put did not do on its own until this fix (found via
// internal/recipe's TestConcurrentResolveAndExecuteAgainstStaleRecipeDoesNotRace,
// task punokawan-q9r.7 #5: two concurrent Store.Put calls against the same
// row surfaced this as a hard failure).
//
// This lives at the Store layer rather than in each caller (internal/recipe,
// internal/roles/{bagong,semar,gareng,petruk}, internal/tasks,
// internal/mcpserver all call Put directly) because every caller of Put is
// equally exposed to the same transient Dolt behavior - fixing it once
// here, instead of wrapping it per-package, is what actually closes the
// gap for all of them.
const mysqlDeadlockOrSerializationFailure = 1213

// isRetryableStoreError reports whether err from a Store write is a
// transient Dolt/MySQL transaction conflict worth retrying, as opposed to
// a real data problem (a provenance validation failure, a genuine
// constraint violation) that would only fail identically on retry.
func isRetryableStoreError(err error) bool {
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number == mysqlDeadlockOrSerializationFailure
	}
	return false
}

// withConflictRetry retries write a bounded number of times when it fails
// with a transient Dolt serialization conflict. A small fixed number of
// attempts with a short jittered sleep is enough: this resolves a
// same-row write race between two in-process goroutines or two Punakawan
// processes, not a provider outage, so an aggressive backoff schedule
// (as used for retrying provider HTTP calls) would be unnecessarily slow
// here.
func withConflictRetry(write func() error) error {
	const maxAttempts = 5
	const baseDelay = 10 * time.Millisecond

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = write()
		if lastErr == nil {
			return nil
		}
		if !isRetryableStoreError(lastErr) || attempt == maxAttempts {
			return lastErr
		}
		time.Sleep(baseDelay * time.Duration(attempt))
	}
	return lastErr
}
