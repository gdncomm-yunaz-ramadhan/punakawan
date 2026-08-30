// Package outbox is the one durable, provider-neutral queue every write a
// domain service makes against an external provider (Jira, GitHub, ...)
// passes through. A raw adapter call is validated once, before it is
// admitted here (internal/providerwrite); after that this package owns the
// row's entire lifecycle - claim, success, retry, ambiguity, and
// cancellation - so a write can be applied exactly once even though workers
// crash, processes restart, and remote responses go missing.
//
// Every intent carries a caller-derived operation_fingerprint that is
// globally unique: enqueuing the same logical effect twice (a retried
// caller, an at-least-once lifecycle hook) resolves to the same row instead
// of racing a second attempt at the same remote mutation.
package outbox

import "time"

// Status is an intent's current position in its lifecycle.
type Status string

const (
	// StatusPending is newly enqueued, not yet claimed by any worker.
	StatusPending Status = "pending"
	// StatusClaimed is currently leased to exactly one worker, which is
	// either about to attempt it or has an attempt in flight.
	StatusClaimed Status = "claimed"
	// StatusRetrying failed with a retryable outcome and is scheduled to be
	// reclaimed at NextAttemptAt.
	StatusRetrying Status = "retrying"
	// StatusSucceeded is terminal: the write landed exactly once and will
	// never be claimed again.
	StatusSucceeded Status = "succeeded"
	// StatusFailed is terminal: the write was rejected in a way retrying
	// cannot fix (validation, permanent provider rejection).
	StatusFailed Status = "failed"
	// StatusCancelled is terminal: a caller withdrew the intent before it
	// succeeded. Cancellation can never move a succeeded intent backward.
	StatusCancelled Status = "cancelled"
	// StatusReconciling means an attempt's outcome was ambiguous (the
	// remote call may or may not have applied) and the intent is waiting on
	// operation-specific reconciliation before it may ever be retried -
	// never blindly replayed.
	StatusReconciling Status = "reconciling"
)

// Intent is one durable, exactly-once provider write.
type Intent struct {
	ID                   string
	OrchestrationID      string
	ExecutionID          string
	SessionID            string
	AdapterID            string
	Operation            string
	TargetKey            string
	PayloadJSON          string
	OperationFingerprint string
	Status               Status
	ClaimOwner           string
	ClaimUntil           *time.Time
	AttemptCount         int
	NextAttemptAt        *time.Time
	ExternalID           string
	ProviderRequestID    string
	LastErrorCode        string
	LastErrorRedacted    string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// Effect is one granular, idempotent side effect an intent's successful
// attempt produced (e.g. one of several subtask keys a single
// jira.create-subtask intent created or found already existing). Recording
// each under its own EffectKey lets a caller detect exactly which effects
// already landed without re-deriving them from the raw provider response.
type Effect struct {
	IntentID    string
	EffectKey   string
	ExternalID  string
	CompletedAt time.Time
}

// Attempt is one durable record of a worker's try at an intent, kept for
// operator visibility and reconciliation context; it is never read back to
// decide behavior (Intent.Status is authoritative for that).
type Attempt struct {
	IntentID           string
	Attempt            int
	WorkerID           string
	StartedAt          time.Time
	FinishedAt         *time.Time
	Outcome            string
	ProviderRequestID  string
	DiagnosticRedacted string
}
