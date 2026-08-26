package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// DeliveryCase is the lifetime internal record for one exact Jira source. Its
// executions may finish and continue, but its identity never changes.
type DeliveryCase struct {
	ID            string    `json:"id"`
	JiraSourceKey string    `json:"jira_source_key"`
	JiraIssueKey  string    `json:"jira_issue_key"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

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

type JiraSourceSnapshot struct {
	ID           string    `json:"id"`
	CaseID       string    `json:"case_id"`
	ExecutionID  string    `json:"execution_id"`
	SessionID    string    `json:"session_id,omitempty"`
	JiraIssueKey string    `json:"jira_issue_key"`
	Version      int       `json:"version"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	ContentHash  string    `json:"content_hash"`
	CapturedAt   time.Time `json:"captured_at"`
}

type JiraAssessment struct {
	ID          string    `json:"id"`
	CaseID      string    `json:"case_id"`
	ExecutionID string    `json:"execution_id"`
	SessionID   string    `json:"session_id,omitempty"`
	SnapshotID  string    `json:"snapshot_id,omitempty"`
	Clarity     string    `json:"clarity"`
	Approval    string    `json:"approval"`
	Rationale   string    `json:"rationale"`
	AssessedAt  time.Time `json:"assessed_at"`
}

type JiraWorkItemMapping struct {
	ID                  string    `json:"id"`
	CaseID              string    `json:"case_id"`
	ExecutionID         string    `json:"execution_id"`
	SessionID           string    `json:"session_id,omitempty"`
	OrchestrationID     string    `json:"orchestration_id"`
	ParentTaskID        string    `json:"parent_task_id"`
	RequirementSourceID string    `json:"requirement_source_id"`
	JiraIssueKey        string    `json:"jira_issue_key"`
	CreatedAt           time.Time `json:"created_at"`
}

type JiraWriteIntent struct {
	ID             string         `json:"id"`
	CaseID         string         `json:"case_id"`
	ExecutionID    string         `json:"execution_id"`
	SessionID      string         `json:"session_id,omitempty"`
	JiraIssueKey   string         `json:"jira_issue_key"`
	Action         string         `json:"action"`
	Payload        map[string]any `json:"payload"`
	IdempotencyKey string         `json:"idempotency_key"`
	Status         string         `json:"status"`
	AttemptCount   int            `json:"attempt_count"`
	RetryAt        *time.Time     `json:"retry_at,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
	ExternalID     string         `json:"external_id,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
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
	Case                DeliveryCase          `json:"case"`
	Execution           DeliveryExecution     `json:"execution"`
	Sessions            []DeliverySession     `json:"sessions"`
	Checkpoints         []SessionCheckpoint   `json:"checkpoints"`
	Usage               []UsageEntry          `json:"usage"`
	Budgets             []DeliveryBudget      `json:"budgets"`
	Snapshots           []JiraSourceSnapshot  `json:"jira_snapshots"`
	Assessments         []JiraAssessment      `json:"jira_assessments"`
	WorkItems           []JiraWorkItemMapping `json:"jira_work_items"`
	WriteIntents        []JiraWriteIntent     `json:"jira_write_intents"`
	Progress            []ProgressReport      `json:"progress_reports"`
	KnownCostByCurrency map[string]float64    `json:"known_cost_by_currency"`
	UnknownPriced       bool                  `json:"unknown_priced_usage"`
}

type ResolveJiraDeliveryOptions struct {
	Title                string
	Description          string
	WorkflowDefinitionID string
	SnapshotTitle        string
	SnapshotBody         string
}

type ResolvedJiraDelivery struct {
	Case      *DeliveryCase      `json:"case"`
	Execution *DeliveryExecution `json:"execution"`
	Created   bool               `json:"created"`
}

// ResolveJiraDelivery resolves the exact normalized Jira key to one lifetime
// case. An open execution is reused; only a terminal execution starts the next
// ordinal under that same case, never a second active case.
func (s *Store) ResolveJiraDelivery(ctx context.Context, idempotencyKey, jiraIssueKey string, opts ResolveJiraDeliveryOptions) (*ResolvedJiraDelivery, error) {
	canonicalKey, issueKey, err := canonicalJiraSource(jiraIssueKey)
	if err != nil {
		return nil, err
	}
	snapshotTitle := strings.TrimSpace(opts.SnapshotTitle)
	if snapshotTitle == "" {
		snapshotTitle = issueKey
	}
	if opts.WorkflowDefinitionID != "" {
		if s.workflowDefinitions == nil {
			return nil, fmt.Errorf("delivery: workflow_definition_id %q given but no workflow definition resolver is configured", opts.WorkflowDefinitionID)
		}
		if err := s.workflowDefinitions.ValidateEnabled(ctx, opts.WorkflowDefinitionID); err != nil {
			return nil, fmt.Errorf("delivery: attach workflow definition %q: %w", opts.WorkflowDefinitionID, err)
		}
	}
	var caseID, executionID, orchestrationID string
	created := false
	now := time.Now().UTC()
	err = s.db.Write(ctx, idempotencyKey, "resolve Jira delivery "+canonicalKey, func(tx *sql.Tx) error {
		var existingCase string
		err := tx.QueryRowContext(ctx, `SELECT id FROM delivery_cases WHERE jira_source_key = ?`, canonicalKey).Scan(&existingCase)
		if errors.Is(err, sql.ErrNoRows) {
			caseID = newID()
			if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_cases (id, jira_source_key, jira_issue_key, status, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?)`, caseID, canonicalKey, issueKey, now.Format(timeLayout), now.Format(timeLayout)); err != nil {
				return err
			}
		} else if err != nil {
			return fmt.Errorf("delivery: load Jira case: %w", err)
		} else {
			caseID = existingCase
		}

		var last DeliveryExecution
		err = scanExecution(tx.QueryRowContext(ctx, `SELECT id, case_id, orchestration_id, ordinal, status, session_id, started_at, ended_at FROM delivery_executions WHERE case_id = ? ORDER BY ordinal DESC LIMIT 1`, caseID), &last)
		if err == nil {
			events, err := loadEventsTx(ctx, tx, last.OrchestrationID)
			if err != nil {
				return err
			}
			orch, err := reduceOrchestration(last.OrchestrationID, events)
			if err != nil {
				return err
			}
			if !isTerminal(orch.Status) {
				executionID, orchestrationID = last.ID, last.OrchestrationID
				return nil
			}
			status := string(orch.Status)
			if _, err := tx.ExecContext(ctx, `UPDATE delivery_executions SET status = ?, ended_at = COALESCE(ended_at, ?) WHERE id = ?`, status, now.Format(timeLayout), last.ID); err != nil {
				return err
			}
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}

		ordinal := 1
		_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal), 0) + 1 FROM delivery_executions WHERE case_id = ?`, caseID).Scan(&ordinal)
		executionID, orchestrationID = newID(), newID()
		payloadMap := map[string]any{"unresolved_inputs": []protocol.DeliveryOrchestrationUnresolvedInputsElem{}}
		if title := strings.TrimSpace(opts.Title); title != "" {
			payloadMap["title"] = title
		}
		if description := strings.TrimSpace(opts.Description); description != "" {
			payloadMap["description"] = description
		}
		if opts.WorkflowDefinitionID != "" {
			payloadMap["workflow_definition_id"] = opts.WorkflowDefinitionID
		}
		payload, err := json.Marshal(payloadMap)
		if err != nil {
			return err
		}
		if err := insertEvent(ctx, tx, eventRow{ID: newID(), OrchestrationID: orchestrationID, IdempotencyKey: idempotencyKey, Type: string(protocol.DeliveryEventTypeOrchestrationCreated), Payload: string(payload), Sequence: 0, OccurredAt: now}); err != nil {
			return err
		}
		sourceID := newID()
		sourcePayload, err := json.Marshal(map[string]any{"provider": "jira", "external_id": issueKey, "canonical_key": canonicalKey, "content_hash": contentHash(SourceInput{Provider: "jira", ExternalID: issueKey, Title: snapshotTitle, Summary: opts.SnapshotBody}), "title": snapshotTitle, "summary": opts.SnapshotBody})
		if err != nil {
			return err
		}
		if err := insertEvent(ctx, tx, eventRow{ID: newID(), OrchestrationID: orchestrationID, EntityID: &sourceID, IdempotencyKey: idempotencyKey, Type: string(protocol.DeliveryEventTypeRequirementCaptured), Payload: string(sourcePayload), Sequence: 1, OccurredAt: now}); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_executions (id, case_id, orchestration_id, ordinal, status, started_at) VALUES (?, ?, ?, ?, 'active', ?)`, executionID, caseID, orchestrationID, ordinal, now.Format(timeLayout)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE delivery_cases SET status = 'active', updated_at = ? WHERE id = ?`, now.Format(timeLayout), caseID); err != nil {
			return err
		}
		var snapshotVersion int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM jira_source_snapshots WHERE case_id = ?`, caseID).Scan(&snapshotVersion); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO jira_source_snapshots (id, idempotency_key, case_id, execution_id, session_id, jira_issue_key, version, title, body, content_hash, captured_at) VALUES (?, ?, ?, ?, '', ?, ?, ?, ?, ?, ?)`, newID(), idempotencyKey+":initial-snapshot", caseID, executionID, issueKey, snapshotVersion, snapshotTitle, opts.SnapshotBody, contentHash(SourceInput{Provider: "jira", ExternalID: issueKey, Title: snapshotTitle, Summary: opts.SnapshotBody}), now.Format(timeLayout)); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, fmt.Errorf("delivery: resolve Jira delivery: %w", err)
	}
	caseRecord, err := s.GetDeliveryCaseByJira(ctx, issueKey)
	if err != nil {
		return nil, err
	}
	exec, err := s.GetExecutionByCase(ctx, caseRecord.ID)
	if err != nil {
		return nil, err
	}
	return &ResolvedJiraDelivery{Case: caseRecord, Execution: exec, Created: created}, nil
}

// CompleteOrchestration marks an execution terminal without ending its
// lifetime case; the next ResolveJiraDelivery call opens the next ordinal.
func (s *Store) CompleteOrchestration(ctx context.Context, idempotencyKey, orchestrationID string, expectedRevision int) (*protocol.DeliveryOrchestration, error) {
	return s.appendOrchestrationEvent(ctx, idempotencyKey, orchestrationID, expectedRevision, protocol.DeliveryEventTypeOrchestrationCompleted, map[string]interface{}{})
}

func canonicalJiraSource(issueKey string) (string, string, error) {
	key := strings.ToUpper(strings.TrimSpace(issueKey))
	if !jiraKeyPattern.MatchString(key) {
		return "", "", fmt.Errorf("delivery: invalid Jira issue key %q", issueKey)
	}
	return "jira:" + key, key, nil
}

func (s *Store) GetDeliveryCaseByJira(ctx context.Context, jiraIssueKey string) (*DeliveryCase, error) {
	key, _, err := canonicalJiraSource(jiraIssueKey)
	if err != nil {
		return nil, err
	}
	return scanCase(s.db.Reader().QueryRowContext(ctx, `SELECT id, jira_source_key, jira_issue_key, status, created_at, updated_at FROM delivery_cases WHERE jira_source_key = ?`, key))
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
func (s *Store) StartSession(ctx context.Context, idempotencyKey, executionID, id, participant, resumedFromID string) (*DeliverySession, error) {
	if strings.TrimSpace(participant) == "" {
		return nil, fmt.Errorf("delivery: session participant is required")
	}
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
		out = DeliverySession{ID: id, CaseID: execution.CaseID, ExecutionID: execution.ID, OrchestrationID: execution.OrchestrationID, ResumedFromID: resumedFromID, Participant: strings.TrimSpace(participant), Status: "active", StartedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_sessions (id, case_id, execution_id, orchestration_id, resumed_from_id, participant, status, started_at) VALUES (?, ?, ?, ?, ?, ?, 'active', ?)`, out.ID, out.CaseID, out.ExecutionID, out.OrchestrationID, out.ResumedFromID, out.Participant, now.Format(timeLayout)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE delivery_executions SET session_id = ? WHERE id = ?`, id, executionID)
		return err
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetSession(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("delivery: start session: %w", err)
	}
	return &out, nil
}

func (s *Store) GetSession(ctx context.Context, id string) (*DeliverySession, error) {
	return scanSession(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, execution_id, orchestration_id, resumed_from_id, participant, status, started_at, ended_at FROM delivery_sessions WHERE id = ?`, id))
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
		session, err := scanSession(tx.QueryRowContext(ctx, `SELECT id, case_id, execution_id, orchestration_id, resumed_from_id, participant, status, started_at, ended_at FROM delivery_sessions WHERE id = ?`, sessionID))
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
	if id == "" {
		id = newID()
	}
	var out UsageEntry
	now := time.Now().UTC()
	err := s.db.Write(ctx, idempotencyKey, "record delivery usage "+id, func(tx *sql.Tx) error {
		session, err := scanSession(tx.QueryRowContext(ctx, `SELECT id, case_id, execution_id, orchestration_id, resumed_from_id, participant, status, started_at, ended_at FROM delivery_sessions WHERE id = ?`, sessionID))
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

func (s *Store) CaptureJiraSnapshot(ctx context.Context, idempotencyKey, executionID, sessionID, title, body string) (*JiraSourceSnapshot, error) {
	var out JiraSourceSnapshot
	now := time.Now().UTC()
	err := s.db.Write(ctx, idempotencyKey, "capture Jira source snapshot "+executionID, func(tx *sql.Tx) error {
		var exec DeliveryExecution
		if err := scanExecution(tx.QueryRowContext(ctx, `SELECT id, case_id, orchestration_id, ordinal, status, session_id, started_at, ended_at FROM delivery_executions WHERE id = ?`, executionID), &exec); err != nil {
			return err
		}
		if sessionID != "" {
			if err := requireSessionScope(ctx, tx, sessionID, &exec); err != nil {
				return err
			}
		}
		var version int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM jira_source_snapshots WHERE case_id = ?`, exec.CaseID).Scan(&version); err != nil {
			return err
		}
		hash := contentHash(SourceInput{Provider: "jira", ExternalID: "snapshot", Title: title, Summary: body})
		out = JiraSourceSnapshot{ID: newID(), CaseID: exec.CaseID, ExecutionID: exec.ID, SessionID: sessionID, JiraIssueKey: "", Version: version, Title: title, Body: body, ContentHash: hash, CapturedAt: now}
		if err := tx.QueryRowContext(ctx, `SELECT jira_issue_key FROM delivery_cases WHERE id = ?`, exec.CaseID).Scan(&out.JiraIssueKey); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO jira_source_snapshots (id, idempotency_key, case_id, execution_id, session_id, jira_issue_key, version, title, body, content_hash, captured_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, out.ID, idempotencyKey, out.CaseID, out.ExecutionID, out.SessionID, out.JiraIssueKey, out.Version, out.Title, out.Body, out.ContentHash, now.Format(timeLayout))
		return err
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return scanSnapshot(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, execution_id, session_id, jira_issue_key, version, title, body, content_hash, captured_at FROM jira_source_snapshots WHERE idempotency_key = ?`, idempotencyKey))
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Store) AssessJira(ctx context.Context, idempotencyKey, executionID, sessionID, snapshotID, clarity, approval, rationale string) (*JiraAssessment, error) {
	if clarity != "clear" && clarity != "needs_clarification" && clarity != "blocked" {
		return nil, fmt.Errorf("delivery: invalid Jira clarity")
	}
	if approval != "not_required" && approval != "pending" && approval != "approved" && approval != "rejected" {
		return nil, fmt.Errorf("delivery: invalid Jira approval")
	}
	if strings.TrimSpace(rationale) == "" {
		return nil, fmt.Errorf("delivery: assessment rationale is required")
	}
	var out JiraAssessment
	now := time.Now().UTC()
	err := s.db.Write(ctx, idempotencyKey, "assess Jira delivery "+executionID, func(tx *sql.Tx) error {
		var exec DeliveryExecution
		if err := scanExecution(tx.QueryRowContext(ctx, `SELECT id, case_id, orchestration_id, ordinal, status, session_id, started_at, ended_at FROM delivery_executions WHERE id = ?`, executionID), &exec); err != nil {
			return err
		}
		if sessionID != "" {
			if err := requireSessionScope(ctx, tx, sessionID, &exec); err != nil {
				return err
			}
		}
		if snapshotID != "" {
			var found int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM jira_source_snapshots WHERE id = ? AND case_id = ?`, snapshotID, exec.CaseID).Scan(&found); err != nil {
				return err
			}
			if found == 0 {
				return ErrScopeMismatch
			}
		}
		out = JiraAssessment{ID: newID(), CaseID: exec.CaseID, ExecutionID: exec.ID, SessionID: sessionID, SnapshotID: snapshotID, Clarity: clarity, Approval: approval, Rationale: strings.TrimSpace(rationale), AssessedAt: now}
		_, err := tx.ExecContext(ctx, `INSERT INTO jira_assessments (id, case_id, execution_id, session_id, snapshot_id, clarity, approval, rationale, assessed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, out.ID, out.CaseID, out.ExecutionID, out.SessionID, out.SnapshotID, out.Clarity, out.Approval, out.Rationale, now.Format(timeLayout))
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// MapWorkItemToJiraTask permits a Jira task only when it is an exact Jira
// requirement already grouped by that parent task; arbitrary issue keys are
// rejected rather than becoming untraceable worklog or write targets.
func (s *Store) MapWorkItemToJiraTask(ctx context.Context, idempotencyKey, executionID, sessionID, parentTaskID, requirementSourceID, jiraIssueKey string) (*JiraWorkItemMapping, error) {
	_, issueKey, err := canonicalJiraSource(jiraIssueKey)
	if err != nil {
		return nil, err
	}
	var out JiraWorkItemMapping
	now := time.Now().UTC()
	err = s.db.Write(ctx, idempotencyKey, "map delivery work item to Jira "+parentTaskID, func(tx *sql.Tx) error {
		var exec DeliveryExecution
		if err := scanExecution(tx.QueryRowContext(ctx, `SELECT id, case_id, orchestration_id, ordinal, status, session_id, started_at, ended_at FROM delivery_executions WHERE id = ?`, executionID), &exec); err != nil {
			return err
		}
		if sessionID != "" {
			if err := requireSessionScope(ctx, tx, sessionID, &exec); err != nil {
				return err
			}
		}
		events, err := loadEventsTx(ctx, tx, exec.OrchestrationID)
		if err != nil {
			return err
		}
		task, err := reduceParentTask(exec.OrchestrationID, parentTaskID, events)
		if err != nil {
			return err
		}
		belongs := false
		for _, sourceID := range task.SourceIds {
			if sourceID == requirementSourceID {
				belongs = true
				break
			}
		}
		if !belongs {
			return ErrScopeMismatch
		}
		source, err := reduceRequirementSource(exec.OrchestrationID, requirementSourceID, events)
		if err != nil {
			return err
		}
		if source.Provider != "jira" || source.CanonicalKey != "jira:"+issueKey {
			return ErrScopeMismatch
		}
		out = JiraWorkItemMapping{ID: newID(), CaseID: exec.CaseID, ExecutionID: exec.ID, SessionID: sessionID, OrchestrationID: exec.OrchestrationID, ParentTaskID: parentTaskID, RequirementSourceID: requirementSourceID, JiraIssueKey: issueKey, CreatedAt: now}
		_, err = tx.ExecContext(ctx, `INSERT INTO jira_work_item_mappings (id, case_id, execution_id, session_id, orchestration_id, parent_task_id, requirement_source_id, jira_issue_key, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(orchestration_id, parent_task_id) DO UPDATE SET session_id = excluded.session_id, requirement_source_id = excluded.requirement_source_id, jira_issue_key = excluded.jira_issue_key, created_at = excluded.created_at`, out.ID, out.CaseID, out.ExecutionID, out.SessionID, out.OrchestrationID, out.ParentTaskID, out.RequirementSourceID, out.JiraIssueKey, now.Format(timeLayout))
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Store) CreateJiraWriteIntent(ctx context.Context, idempotencyKey, executionID, sessionID, jiraIssueKey, action string, payload map[string]any) (*JiraWriteIntent, error) {
	_, issueKey, err := canonicalJiraSource(jiraIssueKey)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(action) == "" {
		return nil, fmt.Errorf("delivery: Jira write action is required")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("delivery: encode Jira write intent: %w", err)
	}
	var out JiraWriteIntent
	now := time.Now().UTC()
	err = s.db.Write(ctx, idempotencyKey, "create Jira write intent "+action, func(tx *sql.Tx) error {
		var exec DeliveryExecution
		if err := scanExecution(tx.QueryRowContext(ctx, `SELECT id, case_id, orchestration_id, ordinal, status, session_id, started_at, ended_at FROM delivery_executions WHERE id = ?`, executionID), &exec); err != nil {
			return err
		}
		if sessionID != "" {
			if err := requireSessionScope(ctx, tx, sessionID, &exec); err != nil {
				return err
			}
		}
		out = JiraWriteIntent{ID: newID(), CaseID: exec.CaseID, ExecutionID: exec.ID, SessionID: sessionID, JiraIssueKey: issueKey, Action: strings.TrimSpace(action), Payload: payload, IdempotencyKey: idempotencyKey, Status: "pending", CreatedAt: now, UpdatedAt: now}
		_, err = tx.ExecContext(ctx, `INSERT INTO jira_write_intents (id, case_id, execution_id, session_id, jira_issue_key, action, payload, idempotency_key, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`, out.ID, out.CaseID, out.ExecutionID, out.SessionID, out.JiraIssueKey, out.Action, string(encoded), out.IdempotencyKey, now.Format(timeLayout), now.Format(timeLayout))
		return err
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetJiraWriteIntentByKey(ctx, idempotencyKey)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ResolveJiraWriteIntent records an adapter attempt's durable result. A failed
// attempt has a scheduled retry and remains visible instead of disappearing.
func (s *Store) ResolveJiraWriteIntent(ctx context.Context, idempotencyKey, intentID, externalID, failure string, retryAt *time.Time) (*JiraWriteIntent, error) {
	now := time.Now().UTC()
	var retryValue any
	if retryAt != nil {
		retryValue = retryAt.UTC().Format(timeLayout)
	}
	err := s.db.Write(ctx, idempotencyKey, "resolve Jira write intent "+intentID, func(tx *sql.Tx) error {
		status := "succeeded"
		if strings.TrimSpace(failure) != "" {
			status = "failed"
			if retryAt != nil {
				status = "retrying"
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE jira_write_intents SET status = ?, attempt_count = attempt_count + 1, retry_at = ?, last_error = ?, external_id = ?, updated_at = ? WHERE id = ? AND status != 'succeeded'`, status, retryValue, strings.TrimSpace(failure), strings.TrimSpace(externalID), now.Format(timeLayout), intentID)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			var found int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM jira_write_intents WHERE id = ?`, intentID).Scan(&found); err != nil {
				return err
			}
			if found == 0 {
				return ErrNotFound
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetJiraWriteIntent(ctx, intentID)
}

func (s *Store) GetJiraWriteIntent(ctx context.Context, id string) (*JiraWriteIntent, error) {
	return scanWriteIntent(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, execution_id, session_id, jira_issue_key, action, payload, idempotency_key, status, attempt_count, retry_at, last_error, external_id, created_at, updated_at FROM jira_write_intents WHERE id = ?`, id))
}
func (s *Store) GetJiraWriteIntentByKey(ctx context.Context, key string) (*JiraWriteIntent, error) {
	return scanWriteIntent(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, execution_id, session_id, jira_issue_key, action, payload, idempotency_key, status, attempt_count, retry_at, last_error, external_id, created_at, updated_at FROM jira_write_intents WHERE idempotency_key = ?`, key))
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
		session, err := scanSession(tx.QueryRowContext(ctx, `SELECT id, case_id, execution_id, orchestration_id, resumed_from_id, participant, status, started_at, ended_at FROM delivery_sessions WHERE id = ?`, sessionID))
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

func (s *Store) GetDeliveryLifecycle(ctx context.Context, orchestrationID string) (*DeliveryLifecycle, error) {
	exec, err := s.executionForOrchestration(ctx, orchestrationID)
	if err != nil {
		return nil, err
	}
	caseRecord, err := scanCase(s.db.Reader().QueryRowContext(ctx, `SELECT id, jira_source_key, jira_issue_key, status, created_at, updated_at FROM delivery_cases WHERE id = ?`, exec.CaseID))
	if err != nil {
		return nil, err
	}
	out := &DeliveryLifecycle{Case: *caseRecord, Execution: *exec, Sessions: []DeliverySession{}, Checkpoints: []SessionCheckpoint{}, Usage: []UsageEntry{}, Budgets: []DeliveryBudget{}, Snapshots: []JiraSourceSnapshot{}, Assessments: []JiraAssessment{}, WorkItems: []JiraWorkItemMapping{}, WriteIntents: []JiraWriteIntent{}, Progress: []ProgressReport{}, KnownCostByCurrency: map[string]float64{}}
	if out.Sessions, err = listSessions(ctx, s.db.Reader(), exec.ID); err != nil {
		return nil, err
	}
	if out.Checkpoints, err = listCheckpoints(ctx, s.db.Reader(), exec.ID); err != nil {
		return nil, err
	}
	if out.Usage, err = listUsage(ctx, s.db.Reader(), exec.ID); err != nil {
		return nil, err
	}
	if out.Budgets, err = listBudgets(ctx, s.db.Reader(), exec.ID); err != nil {
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
func scanCase(row lifecycleScanner) (*DeliveryCase, error) {
	var v DeliveryCase
	var created, updated string
	if err := row.Scan(&v.ID, &v.JiraSourceKey, &v.JiraIssueKey, &v.Status, &created, &updated); err != nil {
		return nil, noRow(err)
	}
	var err error
	if v.CreatedAt, err = scanTime(created); err != nil {
		return nil, err
	}
	if v.UpdatedAt, err = scanTime(updated); err != nil {
		return nil, err
	}
	return &v, nil
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
	if err := row.Scan(&v.ID, &v.CaseID, &v.ExecutionID, &v.OrchestrationID, &v.ResumedFromID, &v.Participant, &v.Status, &started, &ended); err != nil {
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
func scanSnapshot(row lifecycleScanner) (*JiraSourceSnapshot, error) {
	var v JiraSourceSnapshot
	var captured string
	if err := row.Scan(&v.ID, &v.CaseID, &v.ExecutionID, &v.SessionID, &v.JiraIssueKey, &v.Version, &v.Title, &v.Body, &v.ContentHash, &captured); err != nil {
		return nil, noRow(err)
	}
	var err error
	v.CapturedAt, err = scanTime(captured)
	return &v, err
}
func scanAssessment(row lifecycleScanner) (*JiraAssessment, error) {
	var v JiraAssessment
	var assessed string
	if err := row.Scan(&v.ID, &v.CaseID, &v.ExecutionID, &v.SessionID, &v.SnapshotID, &v.Clarity, &v.Approval, &v.Rationale, &assessed); err != nil {
		return nil, noRow(err)
	}
	var err error
	v.AssessedAt, err = scanTime(assessed)
	return &v, err
}
func scanWorkItem(row lifecycleScanner) (*JiraWorkItemMapping, error) {
	var v JiraWorkItemMapping
	var created string
	if err := row.Scan(&v.ID, &v.CaseID, &v.ExecutionID, &v.SessionID, &v.OrchestrationID, &v.ParentTaskID, &v.RequirementSourceID, &v.JiraIssueKey, &created); err != nil {
		return nil, noRow(err)
	}
	var err error
	v.CreatedAt, err = scanTime(created)
	return &v, err
}
func scanWriteIntent(row lifecycleScanner) (*JiraWriteIntent, error) {
	var v JiraWriteIntent
	var payload string
	var retry sql.NullString
	var created, updated string
	if err := row.Scan(&v.ID, &v.CaseID, &v.ExecutionID, &v.SessionID, &v.JiraIssueKey, &v.Action, &payload, &v.IdempotencyKey, &v.Status, &v.AttemptCount, &retry, &v.LastError, &v.ExternalID, &created, &updated); err != nil {
		return nil, noRow(err)
	}
	if err := json.Unmarshal([]byte(payload), &v.Payload); err != nil {
		return nil, err
	}
	var err error
	v.RetryAt, err = scanOptionalTime(retry)
	if err != nil {
		return nil, err
	}
	if v.CreatedAt, err = scanTime(created); err != nil {
		return nil, err
	}
	v.UpdatedAt, err = scanTime(updated)
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
	rows, err := q.QueryContext(ctx, `SELECT id, case_id, execution_id, orchestration_id, resumed_from_id, participant, status, started_at, ended_at FROM delivery_sessions WHERE execution_id = ? ORDER BY started_at, id`, executionID)
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
func listSnapshots(ctx context.Context, q querier, caseID string) ([]JiraSourceSnapshot, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, case_id, execution_id, session_id, jira_issue_key, version, title, body, content_hash, captured_at FROM jira_source_snapshots WHERE case_id = ? ORDER BY version`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JiraSourceSnapshot{}
	for rows.Next() {
		v, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}
func listAssessments(ctx context.Context, q querier, executionID string) ([]JiraAssessment, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, case_id, execution_id, session_id, snapshot_id, clarity, approval, rationale, assessed_at FROM jira_assessments WHERE execution_id = ? ORDER BY assessed_at, id`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JiraAssessment{}
	for rows.Next() {
		v, err := scanAssessment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}
func listWorkItems(ctx context.Context, q querier, executionID string) ([]JiraWorkItemMapping, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, case_id, execution_id, session_id, orchestration_id, parent_task_id, requirement_source_id, jira_issue_key, created_at FROM jira_work_item_mappings WHERE execution_id = ? ORDER BY created_at, id`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JiraWorkItemMapping{}
	for rows.Next() {
		v, err := scanWorkItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}
func listWriteIntents(ctx context.Context, q querier, executionID string) ([]JiraWriteIntent, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, case_id, execution_id, session_id, jira_issue_key, action, payload, idempotency_key, status, attempt_count, retry_at, last_error, external_id, created_at, updated_at FROM jira_write_intents WHERE execution_id = ? ORDER BY created_at, id`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JiraWriteIntent{}
	for rows.Next() {
		v, err := scanWriteIntent(rows)
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
