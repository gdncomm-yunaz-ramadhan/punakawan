package delivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

type DeliveryExecution struct {
	ID              string     `json:"id"`
	CaseID          string     `json:"case_id"`
	OrchestrationID string     `json:"orchestration_id"`
	Ordinal         int        `json:"ordinal"`
	Status          string     `json:"status"`
	SessionID       string     `json:"session_id,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
}

type DeliverySession struct {
	ID              string     `json:"id"`
	CaseID          string     `json:"case_id"`
	ExecutionID     string     `json:"execution_id"`
	OrchestrationID string     `json:"orchestration_id"`
	ResumedFromID   string     `json:"resumed_from_id,omitempty"`
	Participant     string     `json:"participant"`
	Status          string     `json:"status"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	WorktreePath    string     `json:"worktree_path,omitempty"`
	Provider        string     `json:"provider,omitempty"`
}

type SessionCheckpoint struct {
	ID              string    `json:"id"`
	CaseID          string    `json:"case_id"`
	ExecutionID     string    `json:"execution_id"`
	SessionID       string    `json:"session_id"`
	Sequence        int       `json:"sequence"`
	Summary         string    `json:"summary"`
	ProgressPercent *float64  `json:"progress_percent,omitempty"`
	HandoffTo       string    `json:"handoff_to,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type UsageEntry struct {
	ID           string    `json:"id"`
	CaseID       string    `json:"case_id"`
	ExecutionID  string    `json:"execution_id"`
	SessionID    string    `json:"session_id"`
	Kind         string    `json:"kind"`
	Category     string    `json:"category"`
	Model        string    `json:"model,omitempty"`
	Quantity     float64   `json:"quantity"`
	Unit         string    `json:"unit"`
	UnitPrice    *float64  `json:"unit_price,omitempty"`
	CostAmount   *float64  `json:"cost_amount,omitempty"`
	CostCurrency string    `json:"cost_currency,omitempty"`
	PriceSource  string    `json:"price_source,omitempty"`
	RecordedAt   time.Time `json:"recorded_at"`
}

type DeliveryBudget struct {
	ID          string    `json:"id"`
	CaseID      string    `json:"case_id"`
	ExecutionID string    `json:"execution_id"`
	SessionID   string    `json:"session_id,omitempty"`
	Category    string    `json:"category,omitempty"`
	Amount      float64   `json:"amount"`
	Currency    string    `json:"currency"`
	CreatedAt   time.Time `json:"created_at"`
}

type ProgressReport struct {
	ID              string    `json:"id"`
	CaseID          string    `json:"case_id"`
	ExecutionID     string    `json:"execution_id"`
	SessionID       string    `json:"session_id"`
	ProgressPercent *float64  `json:"progress_percent,omitempty"`
	Summary         string    `json:"summary"`
	ReportedAt      time.Time `json:"reported_at"`
}

type DeliveryLifecycle struct {
	Case        DeliveryLifetime    `json:"case"`
	Execution   DeliveryExecution   `json:"execution"`
	Sessions    []DeliverySession   `json:"sessions"`
	Checkpoints []SessionCheckpoint `json:"checkpoints"`
	// Usage, KnownCostByCurrency and UnknownPriced read the legacy
	// delivery_usage_ledger, which nothing writes any more (see
	// report_delivery_usage's deprecation): they stay readable so a
	// delivery recorded before the move still reports what it recorded,
	// and are empty for every delivery since. A current delivery's real
	// tokens and cost are on DeliveryView.Telemetry.
	Usage               []UsageEntry          `json:"usage"`
	Snapshots           []JiraSourceSnapshot  `json:"jira_snapshots"`
	Assessments         []JiraAssessment      `json:"jira_assessments"`
	WorkItems           []JiraWorkItemMapping `json:"jira_work_items"`
	WriteIntents        []JiraWriteIntent     `json:"jira_write_intents"`
	Progress            []ProgressReport      `json:"progress_reports"`
	KnownCostByCurrency map[string]float64    `json:"known_cost_by_currency"`
	UnknownPriced       bool                  `json:"unknown_priced_usage"`
}

// CompleteOrchestration atomically completes a delivery execution in one
// transaction: it appends orchestration.completed, marks the execution
// completed and ended, closes every still-active session, and advances
// delivery_projection_versions. It marks the execution terminal without
// ending its lifetime case; the next StartOrResolveExecution call for the
// same Jira source opens the next ordinal. terminalEffects is the seam
// Task 6's durable outbox will fill to enqueue terminal provider writes in
// the same transaction; nil (the default - no Store option sets it yet)
// means there is nothing to enqueue.
func (s *Store) CompleteOrchestration(ctx context.Context, idempotencyKey, orchestrationID string, expectedRevision int) (*protocol.DeliveryOrchestration, error) {
	now := time.Now().UTC()
	err := s.db.Write(ctx, idempotencyKey, "complete orchestration "+orchestrationID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		current, err := reduceOrchestration(orchestrationID, events)
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if isTerminal(current.Status) {
			return ErrInvalidState
		}
		if err := insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeOrchestrationCompleted), Payload: "{}",
			Sequence: len(events), OccurredAt: now,
		}); err != nil {
			return err
		}
		var executionID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM delivery_executions WHERE orchestration_id = ?`, orchestrationID).Scan(&executionID); err != nil {
			return noRow(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE delivery_executions SET status = 'completed', ended_at = COALESCE(ended_at, ?) WHERE id = ?`, now.Format(timeLayout), executionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE delivery_sessions SET status = 'closed', ended_at = COALESCE(ended_at, ?) WHERE execution_id = ? AND status = 'active'`, now.Format(timeLayout), executionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_projection_versions (orchestration_id, revision, updated_at) VALUES (?, 1, ?) ON CONFLICT(orchestration_id) DO UPDATE SET revision = delivery_projection_versions.revision + 1, updated_at = excluded.updated_at`, orchestrationID, now.Format(timeLayout)); err != nil {
			return err
		}
		if s.terminalEffects != nil {
			if err := s.terminalEffects.EnqueueTerminalEffects(ctx, tx, orchestrationID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetOrchestration(ctx, orchestrationID)
}

func (s *Store) GetExecutionByCase(ctx context.Context, caseID string) (*DeliveryExecution, error) {
	var execution DeliveryExecution
	if err := scanExecution(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, orchestration_id, ordinal, status, session_id, started_at, ended_at FROM delivery_executions WHERE case_id = ? ORDER BY ordinal DESC LIMIT 1`, caseID), &execution); err != nil {
		return nil, err
	}
	return &execution, nil
}

// GetExecution returns one exact delivery execution by its durable ID.
func (s *Store) GetExecution(ctx context.Context, id string) (*DeliveryExecution, error) {
	var execution DeliveryExecution
	if err := scanExecution(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, orchestration_id, ordinal, status, session_id, started_at, ended_at FROM delivery_executions WHERE id = ?`, id), &execution); err != nil {
		return nil, err
	}
	return &execution, nil
}

func (s *Store) executionForOrchestration(ctx context.Context, orchestrationID string) (*DeliveryExecution, error) {
	var execution DeliveryExecution
	if err := scanExecution(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, orchestration_id, ordinal, status, session_id, started_at, ended_at FROM delivery_executions WHERE orchestration_id = ?`, orchestrationID), &execution); err != nil {
		return nil, err
	}
	return &execution, nil
}

// StartSession starts a session for an execution. An active session must be
// explicitly handed off or closed before another participant resumes it.
func (s *Store) StartSession(ctx context.Context, idempotencyKey, executionID, id, participant, resumedFromID, worktreePath, provider string) (*DeliverySession, error) {
	if strings.TrimSpace(participant) == "" {
		return nil, fmt.Errorf("delivery: session participant is required")
	}
	reuseActive := id == "" && strings.TrimSpace(resumedFromID) == ""
	if id == "" {
		id = newID()
	}
	var out DeliverySession
	now := time.Now().UTC()
	err := s.db.Write(ctx, idempotencyKey, "start delivery session "+id, func(tx *sql.Tx) error {
		var execution DeliveryExecution
		if err := scanExecution(tx.QueryRowContext(ctx, `SELECT id, case_id, orchestration_id, ordinal, status, session_id, started_at, ended_at FROM delivery_executions WHERE id = ?`, executionID), &execution); err != nil {
			return err
		}
		if execution.Status != "active" {
			return ErrInvalidState
		}
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM delivery_sessions WHERE execution_id = ? AND status = 'active'`, executionID).Scan(&active); err != nil {
			return err
		}
		if active != 0 {
			if reuseActive {
				existing, err := scanSession(tx.QueryRowContext(ctx, `SELECT id, case_id, execution_id, orchestration_id, resumed_from_id, participant, worktree_path, provider, status, started_at, ended_at FROM delivery_sessions WHERE execution_id = ? AND status = 'active' ORDER BY started_at DESC LIMIT 1`, executionID))
				if err != nil {
					return err
				}
				out = *existing
				return nil
			}
			return ErrInvalidState
		}
		if resumedFromID != "" {
			var prior int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM delivery_sessions WHERE id = ? AND execution_id = ? AND status IN ('handed_off', 'closed')`, resumedFromID, executionID).Scan(&prior); err != nil {
				return err
			}
			if prior == 0 {
				return ErrScopeMismatch
			}
		}
		out = DeliverySession{
			ID: id, CaseID: execution.CaseID, ExecutionID: execution.ID, OrchestrationID: execution.OrchestrationID,
			ResumedFromID: resumedFromID, Participant: strings.TrimSpace(participant), WorktreePath: strings.TrimSpace(worktreePath),
			Provider: strings.TrimSpace(provider), Status: "active", StartedAt: now,
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_sessions (id, case_id, execution_id, orchestration_id, resumed_from_id, participant, worktree_path, provider, status, started_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', ?)`, out.ID, out.CaseID, out.ExecutionID, out.OrchestrationID, out.ResumedFromID, out.Participant, out.WorktreePath, out.Provider, now.Format(timeLayout)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE delivery_executions SET session_id = ? WHERE id = ?`, id, executionID)
		return err
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		// When the caller named no id this call minted one, and a
		// duplicate key means the write that actually happened was an
		// earlier call which minted its own - so that id names nothing.
		// The execution's active session is what the caller is asking
		// for; looking up the just-minted id would report not-found for a
		// session that exists.
		if reuseActive {
			return s.getActiveSession(ctx, executionID)
		}
		return s.GetSession(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("delivery: start session: %w", err)
	}
	return &out, nil
}

// getActiveSession reads back the session currently open on executionID.
func (s *Store) getActiveSession(ctx context.Context, executionID string) (*DeliverySession, error) {
	return scanSession(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, execution_id, orchestration_id, resumed_from_id, participant, worktree_path, provider, status, started_at, ended_at FROM delivery_sessions WHERE execution_id = ? AND status = 'active' ORDER BY started_at DESC LIMIT 1`, executionID))
}

func (s *Store) GetSession(ctx context.Context, id string) (*DeliverySession, error) {
	return scanSession(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, execution_id, orchestration_id, resumed_from_id, participant, worktree_path, provider, status, started_at, ended_at FROM delivery_sessions WHERE id = ?`, id))
}

// CheckpointSession records durable handoff-ready state. A non-empty handoff
// recipient atomically ends the current session as handed_off.
func (s *Store) CheckpointSession(ctx context.Context, idempotencyKey, sessionID, id, summary string, progressPercent *float64, handoffTo string) (*SessionCheckpoint, error) {
	if strings.TrimSpace(summary) == "" {
		return nil, fmt.Errorf("delivery: checkpoint summary is required")
	}
	if progressPercent != nil && (*progressPercent < 0 || *progressPercent > 100) {
		return nil, fmt.Errorf("delivery: progress percent must be between 0 and 100")
	}
	if id == "" {
		id = newID()
	}
	var out SessionCheckpoint
	now := time.Now().UTC()
	err := s.db.Write(ctx, idempotencyKey, "checkpoint delivery session "+sessionID, func(tx *sql.Tx) error {
		session, err := scanSession(tx.QueryRowContext(ctx, `SELECT id, case_id, execution_id, orchestration_id, resumed_from_id, participant, worktree_path, provider, status, started_at, ended_at FROM delivery_sessions WHERE id = ?`, sessionID))
		if err != nil {
			return err
		}
		if session.Status != "active" {
			return ErrInvalidState
		}
		var sequence int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM delivery_session_checkpoints WHERE session_id = ?`, sessionID).Scan(&sequence); err != nil {
			return err
		}
		out = SessionCheckpoint{ID: id, CaseID: session.CaseID, ExecutionID: session.ExecutionID, SessionID: sessionID, Sequence: sequence, Summary: strings.TrimSpace(summary), ProgressPercent: progressPercent, HandoffTo: strings.TrimSpace(handoffTo), CreatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_session_checkpoints (id, case_id, execution_id, session_id, sequence, summary, progress_percent, handoff_to, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, out.ID, out.CaseID, out.ExecutionID, out.SessionID, out.Sequence, out.Summary, out.ProgressPercent, out.HandoffTo, now.Format(timeLayout)); err != nil {
			return err
		}
		if out.HandoffTo != "" {
			_, err = tx.ExecContext(ctx, `UPDATE delivery_sessions SET status = 'handed_off', ended_at = ? WHERE id = ?`, now.Format(timeLayout), sessionID)
		}
		return err
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetCheckpoint(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("delivery: checkpoint session: %w", err)
	}
	return &out, nil
}

func (s *Store) GetCheckpoint(ctx context.Context, id string) (*SessionCheckpoint, error) {
	return scanCheckpoint(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, execution_id, session_id, sequence, summary, progress_percent, handoff_to, created_at FROM delivery_session_checkpoints WHERE id = ?`, id))
}

func (s *Store) RecordUsage(ctx context.Context, idempotencyKey, sessionID, id, kind, category, model string, quantity float64, unit string, unitPrice *float64, currency, priceSource string) (*UsageEntry, error) {
	if kind != "estimate" && kind != "actual" {
		return nil, fmt.Errorf("delivery: usage kind must be estimate or actual")
	}
	if strings.TrimSpace(category) == "" || strings.TrimSpace(unit) == "" || quantity < 0 {
		return nil, fmt.Errorf("delivery: usage category, non-negative quantity, and unit are required")
	}
	if unitPrice != nil && *unitPrice < 0 {
		return nil, fmt.Errorf("delivery: unit price cannot be negative")
	}
	if unitPrice != nil && strings.TrimSpace(currency) == "" {
		return nil, fmt.Errorf("delivery: priced usage requires a currency")
	}
	if unitPrice != nil && *unitPrice == 0 && strings.TrimSpace(priceSource) == "" {
		return nil, fmt.Errorf("delivery: zero-priced usage requires a price source")
	}
	if unitPrice == nil {
		currency, priceSource = "", ""
	}
	if id == "" {
		id = newID()
	}
	var out UsageEntry
	now := time.Now().UTC()
	err := s.db.Write(ctx, idempotencyKey, "record delivery usage "+id, func(tx *sql.Tx) error {
		session, err := scanSession(tx.QueryRowContext(ctx, `SELECT id, case_id, execution_id, orchestration_id, resumed_from_id, participant, worktree_path, provider, status, started_at, ended_at FROM delivery_sessions WHERE id = ?`, sessionID))
		if err != nil {
			return err
		}
		if session.Status != "active" {
			return ErrInvalidState
		}
		cost := (*float64)(nil)
		if unitPrice != nil {
			value := quantity * *unitPrice
			cost = &value
		}
		out = UsageEntry{ID: id, CaseID: session.CaseID, ExecutionID: session.ExecutionID, SessionID: sessionID, Kind: kind, Category: strings.TrimSpace(category), Model: strings.TrimSpace(model), Quantity: quantity, Unit: strings.TrimSpace(unit), UnitPrice: unitPrice, CostAmount: cost, CostCurrency: strings.TrimSpace(currency), PriceSource: strings.TrimSpace(priceSource), RecordedAt: now}
		_, err = tx.ExecContext(ctx, `INSERT INTO delivery_usage_ledger (id, case_id, execution_id, session_id, entry_kind, category, model, quantity, unit, unit_price, cost_amount, cost_currency, price_source, recorded_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, out.ID, out.CaseID, out.ExecutionID, out.SessionID, out.Kind, out.Category, out.Model, out.Quantity, out.Unit, out.UnitPrice, out.CostAmount, out.CostCurrency, out.PriceSource, now.Format(timeLayout))
		return err
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetUsage(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("delivery: record usage: %w", err)
	}
	return &out, nil
}

// CorrectUsagePricing enriches an observed usage row without changing its
// immutable measured quantity, unit, model, kind, or recording time.
func (s *Store) CorrectUsagePricing(ctx context.Context, idempotencyKey, sessionID, id string, unitPrice *float64, currency, priceSource string) (*UsageEntry, error) {
	if id == "" {
		return nil, fmt.Errorf("delivery: usage id is required")
	}
	if unitPrice != nil && (*unitPrice < 0 || strings.TrimSpace(currency) == "") {
		return nil, fmt.Errorf("delivery: priced usage requires non-negative price and currency")
	}
	if unitPrice != nil && *unitPrice == 0 && strings.TrimSpace(priceSource) == "" {
		return nil, fmt.Errorf("delivery: zero-priced usage requires a price source")
	}
	var out *UsageEntry
	err := s.db.Write(ctx, idempotencyKey, "correct delivery usage price "+id, func(tx *sql.Tx) error {
		entry, err := scanUsage(tx.QueryRowContext(ctx, `SELECT id, case_id, execution_id, session_id, entry_kind, category, model, quantity, unit, unit_price, cost_amount, cost_currency, price_source, recorded_at FROM delivery_usage_ledger WHERE id = ? AND session_id = ?`, id, sessionID))
		if err != nil {
			return err
		}
		var cost any
		if unitPrice != nil {
			value := entry.Quantity * *unitPrice
			cost = value
		} else {
			currency, priceSource = "", ""
		}
		if _, err := tx.ExecContext(ctx, `UPDATE delivery_usage_ledger SET unit_price = ?, cost_amount = ?, cost_currency = ?, price_source = ? WHERE id = ? AND session_id = ?`, unitPrice, cost, strings.TrimSpace(currency), strings.TrimSpace(priceSource), id, sessionID); err != nil {
			return err
		}
		entry.UnitPrice, entry.CostCurrency, entry.PriceSource = unitPrice, strings.TrimSpace(currency), strings.TrimSpace(priceSource)
		if unitPrice != nil {
			value := entry.Quantity * *unitPrice
			entry.CostAmount = &value
		} else {
			entry.CostAmount = nil
		}
		out = entry
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("delivery: correct usage pricing: %w", err)
	}
	return out, nil
}

func (s *Store) GetUsage(ctx context.Context, id string) (*UsageEntry, error) {
	return scanUsage(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, execution_id, session_id, entry_kind, category, model, quantity, unit, unit_price, cost_amount, cost_currency, price_source, recorded_at FROM delivery_usage_ledger WHERE id = ?`, id))
}

func (s *Store) SetBudget(ctx context.Context, idempotencyKey, executionID, id, category string, amount float64, currency string) (*DeliveryBudget, error) {
	if amount < 0 || strings.TrimSpace(currency) == "" {
		return nil, fmt.Errorf("delivery: non-negative budget amount and currency are required")
	}
	if id == "" {
		id = newID()
	}
	var out DeliveryBudget
	now := time.Now().UTC()
	err := s.db.Write(ctx, idempotencyKey, "set delivery budget "+id, func(tx *sql.Tx) error {
		var exec DeliveryExecution
		if err := scanExecution(tx.QueryRowContext(ctx, `SELECT id, case_id, orchestration_id, ordinal, status, session_id, started_at, ended_at FROM delivery_executions WHERE id = ?`, executionID), &exec); err != nil {
			return err
		}
		out = DeliveryBudget{ID: id, CaseID: exec.CaseID, ExecutionID: executionID, SessionID: exec.SessionID, Category: strings.TrimSpace(category), Amount: amount, Currency: strings.TrimSpace(currency), CreatedAt: now}
		_, err := tx.ExecContext(ctx, `INSERT INTO delivery_budgets (id, case_id, execution_id, session_id, category, amount, currency, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, out.ID, out.CaseID, out.ExecutionID, out.SessionID, out.Category, out.Amount, out.Currency, now.Format(timeLayout))
		return err
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetBudget(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Store) GetBudget(ctx context.Context, id string) (*DeliveryBudget, error) {
	return scanBudget(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, execution_id, session_id, category, amount, currency, created_at FROM delivery_budgets WHERE id = ?`, id))
}

func (s *Store) ReportProgress(ctx context.Context, idempotencyKey, sessionID, id, summary string, progressPercent *float64) (*ProgressReport, error) {
	if strings.TrimSpace(summary) == "" {
		return nil, fmt.Errorf("delivery: progress summary is required")
	}
	if progressPercent != nil && (*progressPercent < 0 || *progressPercent > 100) {
		return nil, fmt.Errorf("delivery: progress percent must be between 0 and 100")
	}
	if id == "" {
		id = newID()
	}
	var out ProgressReport
	now := time.Now().UTC()
	err := s.db.Write(ctx, idempotencyKey, "report delivery progress "+id, func(tx *sql.Tx) error {
		session, err := scanSession(tx.QueryRowContext(ctx, `SELECT id, case_id, execution_id, orchestration_id, resumed_from_id, participant, worktree_path, provider, status, started_at, ended_at FROM delivery_sessions WHERE id = ?`, sessionID))
		if err != nil {
			return err
		}
		out = ProgressReport{ID: id, CaseID: session.CaseID, ExecutionID: session.ExecutionID, SessionID: sessionID, ProgressPercent: progressPercent, Summary: strings.TrimSpace(summary), ReportedAt: now}
		_, err = tx.ExecContext(ctx, `INSERT INTO delivery_progress_reports (id, case_id, execution_id, session_id, progress_percent, summary, reported_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, out.ID, out.CaseID, out.ExecutionID, out.SessionID, out.ProgressPercent, out.Summary, now.Format(timeLayout))
		return err
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetProgress(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}
func (s *Store) GetProgress(ctx context.Context, id string) (*ProgressReport, error) {
	return scanProgress(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, execution_id, session_id, progress_percent, summary, reported_at FROM delivery_progress_reports WHERE id = ?`, id))
}

// GetProjectionRevision returns orchestrationID's current
// delivery_projection_versions revision - the version every panel list and
// detail read must agree on, per the Global Constraints' "list and detail
// use the same projection revision" requirement.
func (s *Store) GetProjectionRevision(ctx context.Context, orchestrationID string) (int, error) {
	var revision int
	err := s.db.Reader().QueryRowContext(ctx, `SELECT revision FROM delivery_projection_versions WHERE orchestration_id = ?`, orchestrationID).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return revision, nil
}

// JiraOrgForDelivery returns the organisation whose Jira site this
// delivery's case lives on, or "" for a delivery whose source names none
// - which is every delivery on a host with a single configured site,
// every non-Jira delivery, and every delivery with no lifetime at all.
// A missing lifetime is not an error here: "this delivery belongs to no
// organisation" is a complete answer, and the caller's next step (route
// the write through the host's single adapter) is the same either way.
//
// It is the one read a caller needs to route a Jira write to the right
// site, so it stays a single row rather than going through
// GetDeliveryLifecycle, which loads every session, checkpoint, usage
// entry, and snapshot the delivery has.
func (s *Store) JiraOrgForDelivery(ctx context.Context, orchestrationID string) (string, error) {
	var org string
	err := s.db.Reader().QueryRowContext(ctx, `
		SELECT c.source_tenant
		FROM delivery_executions e
		JOIN delivery_cases c ON c.id = e.case_id
		WHERE e.orchestration_id = ?`, orchestrationID).Scan(&org)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return org, nil
}

func (s *Store) GetDeliveryLifecycle(ctx context.Context, orchestrationID string) (*DeliveryLifecycle, error) {
	exec, err := s.executionForOrchestration(ctx, orchestrationID)
	if err != nil {
		return nil, err
	}
	caseRecord, err := scanCase(s.db.Reader().QueryRowContext(ctx, `SELECT `+caseColumns+` FROM delivery_cases WHERE id = ?`, exec.CaseID))
	if err != nil {
		return nil, err
	}
	out := &DeliveryLifecycle{Case: *caseRecord, Execution: *exec, Sessions: []DeliverySession{}, Checkpoints: []SessionCheckpoint{}, Usage: []UsageEntry{}, Snapshots: []JiraSourceSnapshot{}, Assessments: []JiraAssessment{}, WorkItems: []JiraWorkItemMapping{}, WriteIntents: []JiraWriteIntent{}, Progress: []ProgressReport{}, KnownCostByCurrency: map[string]float64{}}
	if out.Sessions, err = listSessions(ctx, s.db.Reader(), exec.ID); err != nil {
		return nil, err
	}
	if out.Checkpoints, err = listCheckpoints(ctx, s.db.Reader(), exec.ID); err != nil {
		return nil, err
	}
	if out.Usage, err = listUsage(ctx, s.db.Reader(), exec.ID); err != nil {
		return nil, err
	}
	if out.Snapshots, err = listSnapshots(ctx, s.db.Reader(), exec.CaseID); err != nil {
		return nil, err
	}
	if out.Assessments, err = listAssessments(ctx, s.db.Reader(), exec.ID); err != nil {
		return nil, err
	}
	if out.WorkItems, err = listWorkItems(ctx, s.db.Reader(), exec.ID); err != nil {
		return nil, err
	}
	if out.WriteIntents, err = listWriteIntents(ctx, s.db.Reader(), exec.ID); err != nil {
		return nil, err
	}
	if out.Progress, err = listProgress(ctx, s.db.Reader(), exec.ID); err != nil {
		return nil, err
	}
	for _, usage := range out.Usage {
		if usage.CostAmount == nil {
			out.UnknownPriced = true
		} else {
			out.KnownCostByCurrency[usage.CostCurrency] += *usage.CostAmount
		}
	}
	return out, nil
}

func requireSessionScope(ctx context.Context, tx *sql.Tx, sessionID string, exec *DeliveryExecution) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM delivery_sessions WHERE id = ? AND execution_id = ?`, sessionID, exec.ID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return ErrScopeMismatch
	}
	return nil
}

type lifecycleScanner interface{ Scan(...any) error }

func noRow(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func scanTime(value string) (time.Time, error) { return time.Parse(timeLayout, value) }
func scanOptionalTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	t, err := scanTime(value.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
func scanExecution(row lifecycleScanner, out *DeliveryExecution) error {
	var started string
	var ended sql.NullString
	if err := row.Scan(&out.ID, &out.CaseID, &out.OrchestrationID, &out.Ordinal, &out.Status, &out.SessionID, &started, &ended); err != nil {
		return noRow(err)
	}
	var err error
	if out.StartedAt, err = scanTime(started); err != nil {
		return err
	}
	out.EndedAt, err = scanOptionalTime(ended)
	return err
}
func scanSession(row lifecycleScanner) (*DeliverySession, error) {
	var v DeliverySession
	var started string
	var ended sql.NullString
	if err := row.Scan(&v.ID, &v.CaseID, &v.ExecutionID, &v.OrchestrationID, &v.ResumedFromID, &v.Participant, &v.WorktreePath, &v.Provider, &v.Status, &started, &ended); err != nil {
		return nil, noRow(err)
	}
	var err error
	if v.StartedAt, err = scanTime(started); err != nil {
		return nil, err
	}
	v.EndedAt, err = scanOptionalTime(ended)
	return &v, err
}
func scanCheckpoint(row lifecycleScanner) (*SessionCheckpoint, error) {
	var v SessionCheckpoint
	var progress sql.NullFloat64
	var created string
	if err := row.Scan(&v.ID, &v.CaseID, &v.ExecutionID, &v.SessionID, &v.Sequence, &v.Summary, &progress, &v.HandoffTo, &created); err != nil {
		return nil, noRow(err)
	}
	if progress.Valid {
		v.ProgressPercent = &progress.Float64
	}
	var err error
	v.CreatedAt, err = scanTime(created)
	return &v, err
}
func scanUsage(row lifecycleScanner) (*UsageEntry, error) {
	var v UsageEntry
	var unitPrice, cost sql.NullFloat64
	var recorded string
	if err := row.Scan(&v.ID, &v.CaseID, &v.ExecutionID, &v.SessionID, &v.Kind, &v.Category, &v.Model, &v.Quantity, &v.Unit, &unitPrice, &cost, &v.CostCurrency, &v.PriceSource, &recorded); err != nil {
		return nil, noRow(err)
	}
	if unitPrice.Valid {
		v.UnitPrice = &unitPrice.Float64
	}
	if cost.Valid {
		v.CostAmount = &cost.Float64
	}
	var err error
	v.RecordedAt, err = scanTime(recorded)
	return &v, err
}
func scanBudget(row lifecycleScanner) (*DeliveryBudget, error) {
	var v DeliveryBudget
	var created string
	if err := row.Scan(&v.ID, &v.CaseID, &v.ExecutionID, &v.SessionID, &v.Category, &v.Amount, &v.Currency, &created); err != nil {
		return nil, noRow(err)
	}
	var err error
	v.CreatedAt, err = scanTime(created)
	return &v, err
}
func scanProgress(row lifecycleScanner) (*ProgressReport, error) {
	var v ProgressReport
	var pct sql.NullFloat64
	var reported string
	if err := row.Scan(&v.ID, &v.CaseID, &v.ExecutionID, &v.SessionID, &pct, &v.Summary, &reported); err != nil {
		return nil, noRow(err)
	}
	if pct.Valid {
		v.ProgressPercent = &pct.Float64
	}
	var err error
	v.ReportedAt, err = scanTime(reported)
	return &v, err
}

func listSessions(ctx context.Context, q querier, executionID string) ([]DeliverySession, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, case_id, execution_id, orchestration_id, resumed_from_id, participant, worktree_path, provider, status, started_at, ended_at FROM delivery_sessions WHERE execution_id = ? ORDER BY started_at, id`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeliverySession{}
	for rows.Next() {
		v, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}
func listCheckpoints(ctx context.Context, q querier, executionID string) ([]SessionCheckpoint, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, case_id, execution_id, session_id, sequence, summary, progress_percent, handoff_to, created_at FROM delivery_session_checkpoints WHERE execution_id = ? ORDER BY created_at, id`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionCheckpoint{}
	for rows.Next() {
		v, err := scanCheckpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}
func listUsage(ctx context.Context, q querier, executionID string) ([]UsageEntry, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, case_id, execution_id, session_id, entry_kind, category, model, quantity, unit, unit_price, cost_amount, cost_currency, price_source, recorded_at FROM delivery_usage_ledger WHERE execution_id = ? ORDER BY recorded_at, id`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageEntry{}
	for rows.Next() {
		v, err := scanUsage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}
func listBudgets(ctx context.Context, q querier, executionID string) ([]DeliveryBudget, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, case_id, execution_id, session_id, category, amount, currency, created_at FROM delivery_budgets WHERE execution_id = ? ORDER BY created_at, id`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeliveryBudget{}
	for rows.Next() {
		v, err := scanBudget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}
func listProgress(ctx context.Context, q querier, executionID string) ([]ProgressReport, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, case_id, execution_id, session_id, progress_percent, summary, reported_at FROM delivery_progress_reports WHERE execution_id = ? ORDER BY reported_at, id`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProgressReport{}
	for rows.Next() {
		v, err := scanProgress(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}
