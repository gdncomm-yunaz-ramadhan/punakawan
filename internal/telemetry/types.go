// Package telemetry is the additive, cumulative record of a delivery's
// agent-session usage: one durable agent_sessions row per distinct
// (client_kind, external_session_id), monotonic per-source usage snapshots
// that never regress on replay, and a one-time finalize that closes a
// session and folds its final snapshot in atomically. It exists
// independently of internal/delivery - it has no foreign key into
// delivery_cases/delivery_executions - so a delivery orchestration id is
// just an opaque grouping key here, and no delivery/execution row needs to
// exist for a caller to Begin a session against it.
package telemetry

import "time"

// AgentSession is one durable, client-scoped agent session tracked against
// a delivery orchestration. Two sessions may share the same orchestration
// id (continuation, or two agents working the same delivery); their usage
// totals are additive by design (see TotalsByDelivery). Two sessions never
// share an (orchestration_id) across two different deliveries, since each
// delivery mints its own orchestration id.
type AgentSession struct {
	ID                string
	OrchestrationID   string
	ExecutionID       string
	ClientKind        string
	ExternalSessionID string
	Participant       string
	Provider          string
	Model             string
	WorktreePath      string
	// Status is active, closed (Finalize ran), or abandoned (reserved for a
	// future reconciliation pass; nothing in this package sets it yet).
	Status string
	// TelemetryStatus is complete only once Finalize has closed this
	// session AND every usage snapshot it ever ingested had fully known
	// per-model pricing (see priceSnapshot); otherwise incomplete. It never
	// substitutes a guess for missing pricing - incomplete simply means
	// "cost here is not fully known", not "cost is zero".
	TelemetryStatus string
	StartedAt       time.Time
	StoppedAt       *time.Time
	StopReason      string
}

// BeginRequest starts or resumes one agent session. ExecutionID may be left
// empty when a caller has no narrower execution scope than the delivery
// itself; it then defaults to DeliveryID.
type BeginRequest struct {
	DeliveryID        string
	ExecutionID       string
	ClientKind        string
	ExternalSessionID string
	Participant       string
	Provider          string
	Model             string
	WorktreePath      string
}

// ModelUsage is one model's token usage as observed at snapshot capture
// time - the raw counts a SnapshotRequest preserves verbatim regardless of
// whether that model's price is known.
type ModelUsage struct {
	Model            string
	InputTokens      int64
	OutputTokens     int64
	CacheWriteTokens int64
	CacheReadTokens  int64
}

// CostAmount is an already-known cost, e.g. one a caller observed directly
// from its own provider rather than one this package must resolve from the
// installed pricing catalog.
type CostAmount struct {
	Amount   float64
	Currency string
}

// SnapshotRequest is one source's (the main turn, or one named subagent)
// cumulative usage as of Sequence. IngestSnapshot's monotonic upsert means
// an older or repeated Sequence for the same (SessionID, SourceID) is a
// no-op - the exact behavior a replayed or duplicated hook event needs.
type SnapshotRequest struct {
	SessionID        string
	SourceID         string
	Sequence         int64
	InputTokens      int64
	OutputTokens     int64
	CacheWriteTokens int64
	CacheReadTokens  int64
	ToolCalls        int64
	ElapsedMS        int64
	// ModelUsage drives pricing resolution against the configured Catalog.
	// Left empty, cost stays explicitly unknown (never substituted with
	// zero) because there is no model to price against.
	ModelUsage []ModelUsage
	// ObservedAt is both the snapshot's recorded capture time and the time
	// pricing is resolved as-of. Defaults to time.Now().UTC() when zero.
	ObservedAt time.Time
	// ObservedCost, when set, overrides catalog resolution with an
	// already-known cost (for example one a caller's own provider reported
	// directly) rather than one this package must look up.
	ObservedCost *CostAmount
}

// FinalizeRequest closes a session exactly once, keyed by StopID
// (agent_session_stops' primary key): calling Finalize twice with the same
// StopID is a no-op that returns the same result both times, never a
// second close or a second application of Snapshot.
type FinalizeRequest struct {
	SessionID  string
	StopID     string
	StoppedAt  time.Time
	StopReason string
	// Snapshot, when set, is applied atomically in the same transaction
	// that closes the session - the "final snapshot" a SessionEnd/Stop
	// hook typically carries alongside its own close signal.
	Snapshot *SnapshotRequest
}

// UsageTotals is one delivery's summed, additive-across-sessions counters.
type UsageTotals struct {
	InputTokens      int64
	OutputTokens     int64
	CacheWriteTokens int64
	CacheReadTokens  int64
	ToolCalls        int64
	ElapsedMS        int64
}

// CostTotal is one delivery's summed estimated cost. FullyKnown is false
// whenever any contributing snapshot's cost was unknown or currencies
// disagreed - Amount/Currency still reflect the sum of whatever portion
// was known, never a fabricated total.
type CostTotal struct {
	Amount     float64
	Currency   string
	FullyKnown bool
}

// UsageProjection is TotalsByDelivery's result: one delivery's cumulative
// counters, total token count, and estimated cost (nil when no
// contributing snapshot ever named a cost at all).
type UsageProjection struct {
	OrchestrationID string
	Counters        UsageTotals
	TotalTokens     int64
	EstimatedCost   *CostTotal
	// TelemetryStatus is complete only when every session contributing to
	// this delivery is itself complete (see AgentSession.TelemetryStatus).
	// A delivery with no sessions at all reports complete vacuously.
	TelemetryStatus string
}
