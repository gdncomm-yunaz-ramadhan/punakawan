package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/ygrip/punakawan/internal/storage"
)

const timeLayout = time.RFC3339Nano

// ErrSessionNotFound is returned by any Store method given a session id
// that names no agent_sessions row.
var ErrSessionNotFound = errors.New("telemetry: session not found")

// ErrAlreadyFinalized is returned by Finalize when sessionID is already
// closed under a different stop id than the one this call names (a true
// second close, not a replay of the same stop - see Finalize's own doc
// comment for why a same-stop-id replay never reaches this check).
var ErrAlreadyFinalized = errors.New("telemetry: session already finalized")

func newID() string { return ulid.Make().String() }

// NewEventID mints a filesystem-safe, lexicographically sortable id for a
// spool record's file name (see spool.go) - callers outside this package
// (the hooks CLI) use it so a spool file's name sorts by creation order
// the same way this package's own internal ids do.
func NewEventID() string { return newID() }

// Store is telemetry's persistence boundary: one durable agent_sessions
// row per (client_kind, external_session_id), monotonic per-source usage
// snapshots, and a one-time finalize. It has no foreign key relationship
// to internal/delivery - see the package doc comment.
type Store struct {
	db      *storage.DB
	catalog *Catalog
}

// Option configures an optional Store dependency.
type Option func(*Store)

// WithCatalog overrides the pricing catalog a Store resolves
// SnapshotRequest.ModelUsage against. Without it, NewStore uses the
// process-wide DefaultCatalog().
func WithCatalog(c *Catalog) Option {
	return func(s *Store) { s.catalog = c }
}

// NewStore wraps an opened storage kernel database.
func NewStore(db *storage.DB, opts ...Option) *Store {
	s := &Store{db: db, catalog: DefaultCatalog()}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

const selectSessionColumns = `id, orchestration_id, execution_id, client_kind, external_session_id, participant, role_version, provider, model, worktree_path, status, telemetry_status, started_at, stopped_at, stop_reason`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSession(row rowScanner) (*AgentSession, error) {
	var s AgentSession
	var startedAt string
	var stoppedAt sql.NullString
	if err := row.Scan(&s.ID, &s.OrchestrationID, &s.ExecutionID, &s.ClientKind, &s.ExternalSessionID, &s.Participant, &s.RoleVersion, &s.Provider, &s.Model, &s.WorktreePath, &s.Status, &s.TelemetryStatus, &startedAt, &stoppedAt, &s.StopReason); err != nil {
		return nil, err
	}
	started, err := time.Parse(timeLayout, startedAt)
	if err != nil {
		return nil, fmt.Errorf("telemetry: parse started_at: %w", err)
	}
	s.StartedAt = started
	if stoppedAt.Valid {
		stopped, err := time.Parse(timeLayout, stoppedAt.String)
		if err != nil {
			return nil, fmt.Errorf("telemetry: parse stopped_at: %w", err)
		}
		s.StoppedAt = &stopped
	}
	return &s, nil
}

// Begin starts a new agent session, or returns the already-active one when
// (ClientKind, ExternalSessionID) already names one - the "resume external
// session" half of the Codex/Claude Code SessionStart mapping. Unlike most
// of this codebase's Store methods, Begin does not take a caller-supplied
// idempotency key: its own idempotency is the (client_kind,
// external_session_id) unique index, checked and inserted-if-absent inside
// one transaction. That is safe because storage.DB's writer is a single
// serialized connection - two concurrent Begin calls for the same
// (ClientKind, ExternalSessionID) cannot both observe "absent" before
// either commits.
func (s *Store) Begin(ctx context.Context, req BeginRequest) (AgentSession, error) {
	deliveryID := strings.TrimSpace(req.DeliveryID)
	clientKind := strings.TrimSpace(req.ClientKind)
	externalID := strings.TrimSpace(req.ExternalSessionID)
	if deliveryID == "" {
		return AgentSession{}, fmt.Errorf("telemetry: begin: delivery id is required")
	}
	if clientKind == "" || externalID == "" {
		return AgentSession{}, fmt.Errorf("telemetry: begin: client_kind and external_session_id are required")
	}
	executionID := strings.TrimSpace(req.ExecutionID)
	if executionID == "" {
		executionID = deliveryID
	}

	id := newID()
	now := time.Now().UTC()
	var out AgentSession
	err := s.db.Write(ctx, "telemetry-begin:"+newID(), "begin agent session "+clientKind+":"+externalID, func(tx *sql.Tx) error {
		existing, err := scanSession(tx.QueryRowContext(ctx, `SELECT `+selectSessionColumns+` FROM agent_sessions WHERE client_kind = ? AND external_session_id = ?`, clientKind, externalID))
		if err == nil {
			out = *existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		out = AgentSession{
			ID: id, OrchestrationID: deliveryID, ExecutionID: executionID,
			ClientKind: clientKind, ExternalSessionID: externalID,
			Participant: strings.TrimSpace(req.Participant), RoleVersion: strings.TrimSpace(req.RoleVersion), Provider: strings.TrimSpace(req.Provider),
			Model: strings.TrimSpace(req.Model), WorktreePath: strings.TrimSpace(req.WorktreePath),
			Status: "active", TelemetryStatus: "incomplete", StartedAt: now,
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO agent_sessions (id, orchestration_id, execution_id, client_kind, external_session_id, participant, role_version, provider, model, worktree_path, status, telemetry_status, started_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			out.ID, out.OrchestrationID, out.ExecutionID, out.ClientKind, out.ExternalSessionID, out.Participant, out.RoleVersion, out.Provider, out.Model, out.WorktreePath, out.Status, out.TelemetryStatus, now.Format(timeLayout))
		return err
	})
	if err != nil {
		return AgentSession{}, fmt.Errorf("telemetry: begin session: %w", err)
	}
	return out, nil
}

// GetSession reads one agent session by id.
func (s *Store) GetSession(ctx context.Context, id string) (*AgentSession, error) {
	session, err := scanSession(s.db.Reader().QueryRowContext(ctx, `SELECT `+selectSessionColumns+` FROM agent_sessions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	return session, err
}

// GetSessionByExternalID reads one agent session by its (client_kind,
// external_session_id) unique key - the lookup a client lifecycle hook
// uses when it only knows its own external session id.
func (s *Store) GetSessionByExternalID(ctx context.Context, clientKind, externalSessionID string) (*AgentSession, error) {
	session, err := scanSession(s.db.Reader().QueryRowContext(ctx, `SELECT `+selectSessionColumns+` FROM agent_sessions WHERE client_kind = ? AND external_session_id = ?`, clientKind, externalSessionID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	return session, err
}

// ListActiveByOrchestration lists every still-active session for
// orchestrationID, e.g. for a delivery completion path to best-effort
// finalize any telemetry session it never itself closed.
func (s *Store) ListActiveByOrchestration(ctx context.Context, orchestrationID string) ([]AgentSession, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT `+selectSessionColumns+` FROM agent_sessions WHERE orchestration_id = ? AND status = 'active'`, orchestrationID)
	if err != nil {
		return nil, fmt.Errorf("telemetry: list active sessions: %w", err)
	}
	defer rows.Close()
	var out []AgentSession
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("telemetry: list active sessions: %w", err)
		}
		out = append(out, *session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("telemetry: list active sessions: %w", err)
	}
	return out, nil
}

const upsertSnapshotSQL = `
INSERT INTO agent_usage_snapshots (
  session_id, source_id, sequence, input_tokens, output_tokens,
  cache_write_tokens, cache_read_tokens, tool_calls, elapsed_ms,
  model_usage_json, pricing_json, estimated_cost_json, observed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id, source_id) DO UPDATE SET
  sequence = excluded.sequence,
  input_tokens = excluded.input_tokens,
  output_tokens = excluded.output_tokens,
  cache_write_tokens = excluded.cache_write_tokens,
  cache_read_tokens = excluded.cache_read_tokens,
  tool_calls = excluded.tool_calls,
  elapsed_ms = excluded.elapsed_ms,
  model_usage_json = excluded.model_usage_json,
  pricing_json = excluded.pricing_json,
  estimated_cost_json = excluded.estimated_cost_json,
  observed_at = excluded.observed_at
WHERE excluded.sequence > agent_usage_snapshots.sequence`

// modelUsageEntry, pricingEntry, and estimatedCost are agent_usage_snapshots'
// model_usage_json/pricing_json/estimated_cost_json shapes.
type modelUsageEntry struct {
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens,omitempty"`
	CacheReadTokens  int64  `json:"cache_read_tokens,omitempty"`
}

type pricingEntry struct {
	Model string     `json:"model"`
	Rate  *ModelRate `json:"rate,omitempty"`
	Known bool       `json:"known"`
}

type estimatedCost struct {
	Amount   float64 `json:"amount,omitempty"`
	Currency string  `json:"currency,omitempty"`
	Known    bool    `json:"known"`
}

// priceSnapshot computes model_usage_json/pricing_json/estimated_cost_json
// for req, resolving each named model against s.catalog as of at, unless
// req.ObservedCost already names a known cost directly. complete reports
// whether every named model (or the caller-observed cost) ended up known;
// it never substitutes zero for an unknown cost - unknown stays unknown.
func (s *Store) priceSnapshot(req SnapshotRequest, at time.Time) (modelUsageJSON, pricingJSON, costJSON string, complete bool, err error) {
	models := make([]modelUsageEntry, 0, len(req.ModelUsage))
	for _, mu := range req.ModelUsage {
		models = append(models, modelUsageEntry{Model: mu.Model, InputTokens: mu.InputTokens, OutputTokens: mu.OutputTokens, CacheWriteTokens: mu.CacheWriteTokens, CacheReadTokens: mu.CacheReadTokens})
	}
	modelUsageBytes, err := json.Marshal(models)
	if err != nil {
		return "", "", "", false, err
	}

	if req.ObservedCost != nil {
		pricingBytes, _ := json.Marshal([]pricingEntry{})
		costBytes, err := json.Marshal(estimatedCost{Amount: req.ObservedCost.Amount, Currency: req.ObservedCost.Currency, Known: true})
		if err != nil {
			return "", "", "", false, err
		}
		return string(modelUsageBytes), string(pricingBytes), string(costBytes), true, nil
	}

	if len(req.ModelUsage) == 0 {
		pricingBytes, _ := json.Marshal([]pricingEntry{})
		costBytes, _ := json.Marshal(estimatedCost{Known: false})
		return string(modelUsageBytes), string(pricingBytes), string(costBytes), false, nil
	}

	entries := make([]pricingEntry, 0, len(req.ModelUsage))
	var total float64
	currency := ""
	allKnown := true
	for _, mu := range req.ModelUsage {
		// A pseudo-model like "<synthetic>" is not something any provider
		// bills for, so it contributes nothing and - crucially - does not
		// make the snapshot unpriced. Recording it as an unknown model
		// would take the whole delivery's cost down with it.
		if NonBillableModel(mu.Model) {
			entries = append(entries, pricingEntry{Model: mu.Model, Known: true})
			continue
		}
		rate, ok := s.catalog.Resolve(mu.Model, at)
		if !ok {
			entries = append(entries, pricingEntry{Model: mu.Model, Known: false})
			allKnown = false
			continue
		}
		cost := float64(mu.InputTokens)*rate.InputPerMillion/1e6 + float64(mu.OutputTokens)*rate.OutputPerMillion/1e6
		if rate.CacheWritePerMillion != nil {
			cost += float64(mu.CacheWriteTokens) * *rate.CacheWritePerMillion / 1e6
		}
		if rate.CacheReadPerMillion != nil {
			cost += float64(mu.CacheReadTokens) * *rate.CacheReadPerMillion / 1e6
		}
		if currency == "" {
			currency = rate.Currency
		} else if currency != rate.Currency {
			allKnown = false
		}
		total += cost
		rateCopy := rate
		entries = append(entries, pricingEntry{Model: mu.Model, Rate: &rateCopy, Known: true})
	}
	pricingBytes, err := json.Marshal(entries)
	if err != nil {
		return "", "", "", false, err
	}
	var costBytes []byte
	if allKnown {
		costBytes, err = json.Marshal(estimatedCost{Amount: total, Currency: currency, Known: true})
	} else {
		costBytes, err = json.Marshal(estimatedCost{Known: false})
	}
	if err != nil {
		return "", "", "", false, err
	}
	return string(modelUsageBytes), string(pricingBytes), string(costBytes), allKnown, nil
}

// allSnapshotsPriced reports whether every snapshot stored for sessionID
// resolved a fully known cost. A session with no snapshots at all is
// vacuously priced - there is no usage whose cost could be unknown.
func allSnapshotsPriced(ctx context.Context, tx *sql.Tx, sessionID string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT estimated_cost_json FROM agent_usage_snapshots WHERE session_id = ?`, sessionID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var costJSON sql.NullString
		if err := rows.Scan(&costJSON); err != nil {
			return false, err
		}
		if !costJSON.Valid {
			return false, nil
		}
		var cost estimatedCost
		if err := json.Unmarshal([]byte(costJSON.String), &cost); err != nil {
			return false, fmt.Errorf("telemetry: decode estimated cost: %w", err)
		}
		if !cost.Known {
			return false, nil
		}
	}
	return true, rows.Err()
}

// IngestSnapshot applies one monotonic usage snapshot for req.SessionID's
// req.SourceID: a req.Sequence at or below the currently stored sequence
// for that (session, source) is a no-op, so a replayed or out-of-order
// hook event never regresses totals. It returns the delivery's refreshed
// cumulative projection.
func (s *Store) IngestSnapshot(ctx context.Context, req SnapshotRequest) (UsageProjection, error) {
	sessionID := strings.TrimSpace(req.SessionID)
	sourceID := strings.TrimSpace(req.SourceID)
	if sessionID == "" || sourceID == "" {
		return UsageProjection{}, fmt.Errorf("telemetry: ingest snapshot: session_id and source_id are required")
	}
	if req.Sequence < 0 {
		return UsageProjection{}, fmt.Errorf("telemetry: ingest snapshot: sequence must be non-negative")
	}
	observedAt := req.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}

	var orchestrationID string
	err := s.db.Write(ctx, "telemetry-snapshot:"+newID(), "ingest usage snapshot "+sessionID+":"+sourceID, func(tx *sql.Tx) error {
		session, err := scanSession(tx.QueryRowContext(ctx, `SELECT `+selectSessionColumns+` FROM agent_sessions WHERE id = ?`, sessionID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrSessionNotFound
			}
			return err
		}
		orchestrationID = session.OrchestrationID
		modelUsageJSON, pricingJSON, costJSON, complete, err := s.priceSnapshot(req, observedAt)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, upsertSnapshotSQL,
			sessionID, sourceID, req.Sequence, req.InputTokens, req.OutputTokens, req.CacheWriteTokens, req.CacheReadTokens, req.ToolCalls, req.ElapsedMS,
			modelUsageJSON, pricingJSON, costJSON, observedAt.Format(timeLayout),
		); err != nil {
			return err
		}
		if !complete {
			if _, err := tx.ExecContext(ctx, `UPDATE agent_sessions SET telemetry_status = 'incomplete' WHERE id = ?`, sessionID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_projection_versions (orchestration_id, revision, updated_at) VALUES (?, 1, ?) ON CONFLICT(orchestration_id) DO UPDATE SET revision = delivery_projection_versions.revision + 1, updated_at = excluded.updated_at`, orchestrationID, observedAt.Format(timeLayout)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		// IngestSnapshot always mints a fresh idempotency key (see the
		// db.Write call above), so storage.ErrDuplicateWrite can never
		// occur here - every error is real.
		return UsageProjection{}, fmt.Errorf("telemetry: ingest usage snapshot: %w", err)
	}
	return s.TotalsByDelivery(ctx, orchestrationID)
}

// Finalize closes sessionID exactly once, applying req.Snapshot (if any)
// atomically in the same transaction, inserting the agent_session_stops
// row keyed by req.StopID, and bumping the delivery's
// delivery_projection_versions revision. A second call with the same
// StopID never re-runs any of this - db.Write's own idempotency key
// (derived from StopID) makes it a pure replay that returns the same
// already-applied result. A second call for the same session with a
// different StopID after it is already closed is a real conflict
// (ErrAlreadyFinalized), not a replay.
func (s *Store) Finalize(ctx context.Context, req FinalizeRequest) (AgentSession, UsageProjection, error) {
	sessionID := strings.TrimSpace(req.SessionID)
	stopID := strings.TrimSpace(req.StopID)
	if sessionID == "" || stopID == "" {
		return AgentSession{}, UsageProjection{}, fmt.Errorf("telemetry: finalize: session_id and stop_id are required")
	}
	stoppedAt := req.StoppedAt
	if stoppedAt.IsZero() {
		stoppedAt = time.Now().UTC()
	}
	stopReason := strings.TrimSpace(req.StopReason)

	err := s.db.Write(ctx, "telemetry-finalize:"+stopID, "finalize agent session "+sessionID, func(tx *sql.Tx) error {
		session, err := scanSession(tx.QueryRowContext(ctx, `SELECT `+selectSessionColumns+` FROM agent_sessions WHERE id = ?`, sessionID))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrSessionNotFound
			}
			return err
		}
		if req.Snapshot != nil {
			snap := *req.Snapshot
			snap.SessionID = sessionID
			if strings.TrimSpace(snap.SourceID) == "" {
				return fmt.Errorf("telemetry: finalize: snapshot requires source_id")
			}
			observedAt := snap.ObservedAt
			if observedAt.IsZero() {
				observedAt = stoppedAt
			}
			modelUsageJSON, pricingJSON, costJSON, _, err := s.priceSnapshot(snap, observedAt)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, upsertSnapshotSQL,
				sessionID, strings.TrimSpace(snap.SourceID), snap.Sequence, snap.InputTokens, snap.OutputTokens, snap.CacheWriteTokens, snap.CacheReadTokens, snap.ToolCalls, snap.ElapsedMS,
				modelUsageJSON, pricingJSON, costJSON, observedAt.Format(timeLayout),
			); err != nil {
				return err
			}
		}
		if session.Status != "active" {
			return ErrAlreadyFinalized
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_session_stops (stop_id, session_id, stopped_at, stop_reason) VALUES (?, ?, ?, ?)`, stopID, sessionID, stoppedAt.Format(timeLayout), stopReason); err != nil {
			return err
		}
		// Recompute from every snapshot this session ever stored, not
		// from the final one alone. The session row only ever moved
		// towards incomplete (IngestSnapshot has no upgrade branch), so
		// reading it back here meant a session that hit one unpriceable
		// snapshot could never report complete again - and a finalize
		// carrying no snapshot at all, which is how the stray-session
		// sweep closes a session, simply preserved that.
		complete, err := allSnapshotsPriced(ctx, tx, sessionID)
		if err != nil {
			return err
		}
		telemetryStatus := "incomplete"
		if complete {
			telemetryStatus = "complete"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE agent_sessions SET status = 'closed', stopped_at = ?, stop_reason = ?, telemetry_status = ? WHERE id = ?`, stoppedAt.Format(timeLayout), stopReason, telemetryStatus, sessionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_projection_versions (orchestration_id, revision, updated_at) VALUES (?, 1, ?) ON CONFLICT(orchestration_id) DO UPDATE SET revision = delivery_projection_versions.revision + 1, updated_at = excluded.updated_at`, session.OrchestrationID, stoppedAt.Format(timeLayout)); err != nil {
			return err
		}
		return nil
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return AgentSession{}, UsageProjection{}, fmt.Errorf("telemetry: finalize session: %w", err)
	}
	session, gerr := s.GetSession(ctx, sessionID)
	if gerr != nil {
		return AgentSession{}, UsageProjection{}, gerr
	}
	totals, terr := s.TotalsByDelivery(ctx, session.OrchestrationID)
	if terr != nil {
		return AgentSession{}, UsageProjection{}, terr
	}
	return *session, totals, nil
}

// SnapshotState is one (session, source)'s currently stored cumulative
// counters - the read half a caller needs to turn a delta-style legacy
// report into a correctly cumulative IngestSnapshot call (read the
// current total, add the delta, ingest the new total at sequence+1).
type SnapshotState struct {
	SessionID string
	SourceID  string
	Sequence  int64
	Counters  UsageTotals
}

// GetSnapshot reads (sessionID, sourceID)'s current snapshot state, or
// nil (not an error) when no snapshot has ever been ingested for it yet -
// a genuinely absent baseline, not a failure.
func (s *Store) GetSnapshot(ctx context.Context, sessionID, sourceID string) (*SnapshotState, error) {
	var out SnapshotState
	err := s.db.Reader().QueryRowContext(ctx, `SELECT session_id, source_id, sequence, input_tokens, output_tokens, cache_write_tokens, cache_read_tokens, tool_calls, elapsed_ms FROM agent_usage_snapshots WHERE session_id = ? AND source_id = ?`, sessionID, sourceID).Scan(
		&out.SessionID, &out.SourceID, &out.Sequence,
		&out.Counters.InputTokens, &out.Counters.OutputTokens, &out.Counters.CacheWriteTokens, &out.Counters.CacheReadTokens, &out.Counters.ToolCalls, &out.Counters.ElapsedMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("telemetry: get snapshot: %w", err)
	}
	return &out, nil
}

// TotalsByDelivery sums every session's every source's current snapshot
// for orchestrationID. Totals are additive across every session that
// shares this orchestration id (continuation, or two agents on the same
// delivery); two different orchestration ids never contribute to each
// other's totals.
func (s *Store) TotalsByDelivery(ctx context.Context, orchestrationID string) (UsageProjection, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
		SELECT sess.telemetry_status,
		       COALESCE(snap.input_tokens, 0), COALESCE(snap.output_tokens, 0),
		       COALESCE(snap.cache_write_tokens, 0), COALESCE(snap.cache_read_tokens, 0),
		       COALESCE(snap.tool_calls, 0), COALESCE(snap.elapsed_ms, 0),
		       snap.estimated_cost_json, snap.pricing_json
		FROM agent_sessions sess
		LEFT JOIN agent_usage_snapshots snap ON snap.session_id = sess.id
		WHERE sess.orchestration_id = ?`, orchestrationID)
	if err != nil {
		return UsageProjection{}, fmt.Errorf("telemetry: totals by delivery: %w", err)
	}
	defer rows.Close()

	projection := UsageProjection{OrchestrationID: orchestrationID, TelemetryStatus: "complete"}
	unpricedSeen := map[string]bool{}
	var costKnownAny, costFullyKnown bool
	costFullyKnown = true
	var costTotal float64
	currency := ""
	for rows.Next() {
		var sessionTelemetryStatus string
		var input, output, cacheWrite, cacheRead, toolCalls, elapsed int64
		var costJSON, pricingJSON sql.NullString
		if err := rows.Scan(&sessionTelemetryStatus, &input, &output, &cacheWrite, &cacheRead, &toolCalls, &elapsed, &costJSON, &pricingJSON); err != nil {
			return UsageProjection{}, fmt.Errorf("telemetry: totals by delivery: %w", err)
		}
		// Collect the model ids that failed to resolve. "Cost unknown"
		// on its own gives a reader nothing to act on; the model name is
		// the whole lead - it says which catalog entry is missing.
		if pricingJSON.Valid {
			var entries []pricingEntry
			if err := json.Unmarshal([]byte(pricingJSON.String), &entries); err != nil {
				return UsageProjection{}, fmt.Errorf("telemetry: decode pricing: %w", err)
			}
			for _, entry := range entries {
				if entry.Known || unpricedSeen[entry.Model] {
					continue
				}
				unpricedSeen[entry.Model] = true
				projection.UnpricedModels = append(projection.UnpricedModels, entry.Model)
			}
		}
		if sessionTelemetryStatus != "complete" {
			projection.TelemetryStatus = "incomplete"
		}
		projection.Counters.InputTokens += input
		projection.Counters.OutputTokens += output
		projection.Counters.CacheWriteTokens += cacheWrite
		projection.Counters.CacheReadTokens += cacheRead
		projection.Counters.ToolCalls += toolCalls
		projection.Counters.ElapsedMS += elapsed
		if !costJSON.Valid {
			continue
		}
		var cost estimatedCost
		if err := json.Unmarshal([]byte(costJSON.String), &cost); err != nil {
			return UsageProjection{}, fmt.Errorf("telemetry: decode estimated cost: %w", err)
		}
		if !cost.Known {
			costFullyKnown = false
			continue
		}
		costKnownAny = true
		// A snapshot whose only usage was non-billable is known to have
		// cost nothing and names no currency. It must not claim the
		// delivery's currency slot, or the first real priced snapshot
		// after it reads as a currency mismatch.
		if cost.Currency == "" && cost.Amount == 0 {
			continue
		}
		if currency == "" {
			currency = cost.Currency
		} else if currency != cost.Currency {
			costFullyKnown = false
		}
		costTotal += cost.Amount
	}
	if err := rows.Err(); err != nil {
		return UsageProjection{}, fmt.Errorf("telemetry: totals by delivery: %w", err)
	}
	sort.Strings(projection.UnpricedModels)
	projection.TotalTokens = projection.Counters.InputTokens + projection.Counters.OutputTokens + projection.Counters.CacheWriteTokens + projection.Counters.CacheReadTokens
	if costKnownAny {
		projection.EstimatedCost = &CostTotal{Amount: costTotal, Currency: currency, FullyKnown: costFullyKnown}
	}
	return projection, nil
}

// UnresolvedModels names every model id recorded in the last limit
// snapshots that the pricing catalog still cannot resolve, sorted and
// deduplicated. A model that was unpriced when its snapshot was taken but
// resolves now is left out: the price simply arrived later, and nothing
// can or should retroactively re-cost that snapshot.
//
// It exists so `punakawan doctor` can say that an installed, apparently
// healthy telemetry pipeline is producing usage nothing can price. The
// existing hook check verifies that events reach the spool and the
// database, and reported "complete" throughout the entire period in
// which every snapshot was priced unknown.
func (s *Store) UnresolvedModels(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 200
	}
	now := time.Now().UTC()
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT pricing_json FROM agent_usage_snapshots ORDER BY observed_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("telemetry: list unresolved models: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var pricingJSON sql.NullString
		if err := rows.Scan(&pricingJSON); err != nil {
			return nil, fmt.Errorf("telemetry: list unresolved models: %w", err)
		}
		if !pricingJSON.Valid {
			continue
		}
		var entries []pricingEntry
		if err := json.Unmarshal([]byte(pricingJSON.String), &entries); err != nil {
			return nil, fmt.Errorf("telemetry: decode pricing: %w", err)
		}
		for _, entry := range entries {
			if entry.Known || seen[entry.Model] {
				continue
			}
			seen[entry.Model] = true
			// A model recorded as unpriced that the catalog can price
			// today is a historical row, not something still going
			// wrong - the price arrived after the snapshot did. Only a
			// model that is still unpriceable is actionable, and
			// reporting the rest would leave this check permanently red
			// over rows nothing can retroactively fix. The delivery those
			// rows belong to still reports its own cost as unknown, which
			// is the truthful answer for that delivery.
			if NonBillableModel(entry.Model) {
				continue
			}
			if _, ok := s.catalog.Resolve(entry.Model, now); ok {
				continue
			}
			out = append(out, entry.Model)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("telemetry: list unresolved models: %w", err)
	}
	sort.Strings(out)
	return out, nil
}
