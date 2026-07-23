package recipe

import (
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// mysqlDeadlockOrSerializationFailure is MySQL/Dolt error 1213 (SQLSTATE
// 40001): "Deadlock found when trying to get lock" / Dolt's own
// "serialization failure: this transaction conflicts with a committed
// transaction from another client, try restarting transaction." Both
// report as this same error number and are, by definition, transient -
// the standard advice for both is exactly "retry the transaction",
// which knowledge.Store.Put does not do on its own (confirmed empirically
// by TestConcurrentResolveAndExecuteAgainstVerifiedRecipeDoesNotRace: two
// concurrent Store.Put calls against the same recipe row can otherwise
// surface this as a hard failure from ResolveAndExecute, task q9r.7 #5).
const mysqlDeadlockOrSerializationFailure = 1213

// isRetryableStoreError reports whether err from knowledge.Store.Put (or
// Get/Supersede, which share the same underlying transactional writes) is
// a transient Dolt/MySQL transaction conflict worth retrying, as opposed
// to a real data problem (a provenance validation failure, a genuine
// constraint violation) that would only fail identically on retry.
func isRetryableStoreError(err error) bool {
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number == mysqlDeadlockOrSerializationFailure
	}
	return false
}

// putWithConflictRetry retries Store.Put a bounded number of times when
// the underlying write fails with a transient Dolt serialization
// conflict - the same failure class RetryingSearch/RetryingAgile handle
// for the provider side, applied here to this package's own store writes
// (Executor.recordLastExecution, Repository.Verify's Store.Put) rather
// than a provider HTTP call. A small fixed number of attempts with a short
// jittered sleep is enough: this is resolving a same-row write race
// between two in-process goroutines or two Punakawan processes, not
// riding out a provider outage, so the aggressive backoff schedule
// RetryingSearch uses would be unnecessarily slow here.
func putWithConflictRetry(put func(protocol.KnowledgeRecord) error, rec protocol.KnowledgeRecord) error {
	const maxAttempts = 5
	const baseDelay = 10 * time.Millisecond

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = put(rec)
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
