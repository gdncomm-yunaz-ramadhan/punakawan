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
	// RoleVersion is the internal/agent.RoleSpec.Version this session's
	// Participant resolved to at Begin time (empty when Participant did
	// not name one of the four known roles - best-effort enrichment, not
	// a required input).
	RoleVersion  string
	Provider     string
	Model        string
	WorktreePath string
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
	// RoleVersion is passed through to the created AgentSession's own
	// RoleVersion field verbatim - see AgentSession.RoleVersion.
	RoleVersion  string
	Provider     string
	Model        string
	WorktreePath string
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

// ModelUsageTotals is one model's share of a delivery's usage.
//
// A delivery's cost is driven by which model spent which kind of token -
// a cache read is a twenty-fifth the price of an output token - so a
// single summed figure cannot be checked, attributed, or acted on. Every
// number here is already recorded per snapshot; this is the shape that
// carries it out.
type ModelUsageTotals struct {
	Model            string  `json:"model"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	EstimatedCost    float64 `json:"estimated_cost,omitempty"`
	Currency         string  `json:"currency,omitempty"`
	// Priced is false when the catalog named no rate for this model, in
	// which case EstimatedCost is absent rather than zero.
	Priced bool `json:"priced"`
}

// SessionUsageTotals is one agent session's share of a delivery's usage.
// A delivery worked across several sessions otherwise reports one lump
// that says nothing about which sitting spent it.
type SessionUsageTotals struct {
	SessionID         string `json:"session_id"`
	ExternalSessionID string `json:"external_session_id,omitempty"`
	ClientKind        string `json:"client_kind,omitempty"`
	Participant       string `json:"participant,omitempty"`
	// Status, StartedAt and StoppedAt make this a session row a reader can
	// act on rather than a bare set of counters. A client session that
	// only ever fired lifecycle hooks has no delivery_sessions row at all,
	// so this is the only place it is visible - and it is exactly the
	// session whose usage the delivery is reporting.
	Status           string  `json:"status,omitempty"`
	StartedAt        string  `json:"started_at,omitempty"`
	StoppedAt        string  `json:"stopped_at,omitempty"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	ToolCalls        int64   `json:"tool_calls"`
	ElapsedMS        int64   `json:"elapsed_ms"`
	EstimatedCost    float64 `json:"estimated_cost,omitempty"`
	Currency         string  `json:"currency,omitempty"`
	Priced           bool    `json:"priced"`
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
	// UnpricedModels names, sorted and deduplicated, every model id this
	// delivery's snapshots reported that the pricing catalog could not
	// resolve. It is the actionable half of an unknown cost: without it a
	// reader knows only that cost is unknown, not which model to price.
	// Tagged omitempty because this type is reflected into an MCP tool's
	// output schema: an untagged nil slice serializes as null and fails
	// the generated "array" validation on every delivery that has nothing
	// to report.
	UnpricedModels []string `json:"unpriced_models,omitempty"`
	// ByModel and BySession break Counters down along the two axes a
	// reader actually asks about: what was spent on which model, and in
	// which sitting. Both are omitempty for the same reason as
	// UnpricedModels - a nil slice reflected into an MCP output schema
	// serializes as null and fails its "array" validation.
	ByModel   []ModelUsageTotals   `json:"by_model,omitempty"`
	BySession []SessionUsageTotals `json:"by_session,omitempty"`
}
