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
)

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
	Rationale   string    `json:"rationale"`
	AssessedAt  time.Time `json:"assessed_at"`
}

type JiraWorkItemMapping struct {
	ID                  string     `json:"id"`
	CaseID              string     `json:"case_id"`
	ExecutionID         string     `json:"execution_id"`
	SessionID           string     `json:"session_id,omitempty"`
	OrchestrationID     string     `json:"orchestration_id"`
	ParentTaskID        string     `json:"parent_task_id"`
	RequirementSourceID string     `json:"requirement_source_id"`
	JiraIssueKey        string     `json:"jira_issue_key"`
	CreatedAt           time.Time  `json:"created_at"`
	FirstTouchedAt      *time.Time `json:"first_touched_at,omitempty"`
	LastTouchedAt       *time.Time `json:"last_touched_at,omitempty"`
	TouchCount          int        `json:"touch_count"`
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

func (s *Store) AssessJira(ctx context.Context, idempotencyKey, executionID, sessionID, snapshotID, clarity, rationale string) (*JiraAssessment, error) {
	if clarity != "clear" && clarity != "needs_clarification" {
		return nil, fmt.Errorf("delivery: invalid Jira clarity")
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
		out = JiraAssessment{ID: newID(), CaseID: exec.CaseID, ExecutionID: exec.ID, SessionID: sessionID, SnapshotID: snapshotID, Clarity: clarity, Rationale: strings.TrimSpace(rationale), AssessedAt: now}
		_, err := tx.ExecContext(ctx, `INSERT INTO jira_assessments (id, case_id, execution_id, session_id, snapshot_id, clarity, rationale, assessed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, out.ID, out.CaseID, out.ExecutionID, out.SessionID, out.SnapshotID, out.Clarity, out.Rationale, now.Format(timeLayout))
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
		var tenant string
		if err := tx.QueryRowContext(ctx, `SELECT source_tenant FROM delivery_cases WHERE id = ?`, exec.CaseID).Scan(&tenant); err != nil {
			return err
		}
		expectedKey, err := CanonicalKey(SourceInput{Provider: "jira", ExternalID: issueKey, Tenant: tenant})
		if err != nil {
			return err
		}
		if source.Provider != "jira" || source.CanonicalKey != expectedKey {
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

// TouchJiraWorkItem records that key's caller (typically one MCP tool
// call) engaged with an already-mapped Jira work item, incrementing its
// touch_count at most once per unique (session_id, key) pair - key
// doubles as this call's tool-call identity, the same way every other
// method in this package treats its idempotency key as the identity of
// one exact call. Touching an issue with no existing mapping in this
// orchestration fails closed (ErrNotFound): a touch can only ever
// enrich an already-durable mapping, never invent one.
func (s *Store) TouchJiraWorkItem(ctx context.Context, key, executionID, sessionID, jiraIssueKey string, at time.Time) (*JiraWorkItemMapping, error) {
	_, issueKey, err := canonicalJiraSource(jiraIssueKey)
	if err != nil {
		return nil, err
	}
	var mappingID string
	err = s.db.Write(ctx, key, "touch jira work item "+issueKey, func(tx *sql.Tx) error {
		var exec DeliveryExecution
		if err := scanExecution(tx.QueryRowContext(ctx, `SELECT id, case_id, orchestration_id, ordinal, status, session_id, started_at, ended_at FROM delivery_executions WHERE id = ?`, executionID), &exec); err != nil {
			return err
		}
		if sessionID != "" {
			if err := requireSessionScope(ctx, tx, sessionID, &exec); err != nil {
				return err
			}
		}
		if err := tx.QueryRowContext(ctx, `SELECT id FROM jira_work_item_mappings WHERE orchestration_id = ? AND jira_issue_key = ?`, exec.OrchestrationID, issueKey).Scan(&mappingID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		formatted := at.UTC().Format(timeLayout)
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO jira_work_item_touches (mapping_id, session_id, tool_call_id, touched_at) VALUES (?, ?, ?, ?)`, mappingID, sessionID, key, formatted)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			// Already touched by exactly this (session, tool call) before:
			// nothing new to count.
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE jira_work_item_mappings SET touch_count = touch_count + 1, last_touched_at = ?, first_touched_at = CASE WHEN first_touched_at = '' THEN ? ELSE first_touched_at END WHERE id = ?`, formatted, formatted, mappingID)
		return err
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, fmt.Errorf("delivery: touch Jira work item %s: %w", issueKey, err)
	}
	if mappingID == "" {
		// The duplicate-idempotency-key path never ran the body above, so
		// mappingID must be resolved the same way it would have been.
		var orchestrationID string
		if err := s.db.Reader().QueryRowContext(ctx, `SELECT orchestration_id FROM delivery_executions WHERE id = ?`, executionID).Scan(&orchestrationID); err != nil {
			return nil, noRow(err)
		}
		if err := s.db.Reader().QueryRowContext(ctx, `SELECT id FROM jira_work_item_mappings WHERE orchestration_id = ? AND jira_issue_key = ?`, orchestrationID, issueKey).Scan(&mappingID); err != nil {
			return nil, noRow(err)
		}
	}
	return scanWorkItem(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, execution_id, session_id, orchestration_id, parent_task_id, requirement_source_id, jira_issue_key, created_at, first_touched_at, last_touched_at, touch_count FROM jira_work_item_mappings WHERE id = ?`, mappingID))
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

// CancelJiraWriteIntent prevents a stale pending or retrying intent from being
// executed. Repeating cancellation returns the same durable intent.
func (s *Store) CancelJiraWriteIntent(ctx context.Context, idempotencyKey, intentID string) (*JiraWriteIntent, error) {
	now := time.Now().UTC()
	err := s.db.Write(ctx, idempotencyKey, "cancel Jira write intent "+intentID, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE jira_write_intents SET status = 'cancelled', retry_at = NULL, updated_at = ? WHERE id = ? AND status IN ('pending', 'retrying')`, now.Format(timeLayout), intentID)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 1 {
			return nil
		}
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM jira_write_intents WHERE id = ?`, intentID).Scan(&status); err != nil {
			return noRow(err)
		}
		if status == "cancelled" {
			return nil
		}
		return ErrInvalidState
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetJiraWriteIntent(ctx, intentID)
	}
	if err != nil {
		return nil, fmt.Errorf("delivery: cancel Jira write intent: %w", err)
	}
	return s.GetJiraWriteIntent(ctx, intentID)
}

func (s *Store) GetJiraWriteIntent(ctx context.Context, id string) (*JiraWriteIntent, error) {
	return scanWriteIntent(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, execution_id, session_id, jira_issue_key, action, payload, idempotency_key, status, attempt_count, retry_at, last_error, external_id, created_at, updated_at FROM jira_write_intents WHERE id = ?`, id))
}
func (s *Store) GetJiraWriteIntentByKey(ctx context.Context, key string) (*JiraWriteIntent, error) {
	return scanWriteIntent(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, execution_id, session_id, jira_issue_key, action, payload, idempotency_key, status, attempt_count, retry_at, last_error, external_id, created_at, updated_at FROM jira_write_intents WHERE idempotency_key = ?`, key))
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
	if err := row.Scan(&v.ID, &v.CaseID, &v.ExecutionID, &v.SessionID, &v.SnapshotID, &v.Clarity, &v.Rationale, &assessed); err != nil {
		return nil, noRow(err)
	}
	var err error
	v.AssessedAt, err = scanTime(assessed)
	return &v, err
}
func scanWorkItem(row lifecycleScanner) (*JiraWorkItemMapping, error) {
	var v JiraWorkItemMapping
	var created string
	var firstTouched, lastTouched sql.NullString
	if err := row.Scan(&v.ID, &v.CaseID, &v.ExecutionID, &v.SessionID, &v.OrchestrationID, &v.ParentTaskID, &v.RequirementSourceID, &v.JiraIssueKey, &created, &firstTouched, &lastTouched, &v.TouchCount); err != nil {
		return nil, noRow(err)
	}
	var err error
	v.CreatedAt, err = scanTime(created)
	if err != nil {
		return nil, err
	}
	if v.FirstTouchedAt, err = scanOptionalTime(firstTouched); err != nil {
		return nil, err
	}
	if v.LastTouchedAt, err = scanOptionalTime(lastTouched); err != nil {
		return nil, err
	}
	return &v, nil
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
	rows, err := q.QueryContext(ctx, `SELECT id, case_id, execution_id, session_id, snapshot_id, clarity, rationale, assessed_at FROM jira_assessments WHERE execution_id = ? ORDER BY assessed_at, id`, executionID)
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
	rows, err := q.QueryContext(ctx, `SELECT id, case_id, execution_id, session_id, orchestration_id, parent_task_id, requirement_source_id, jira_issue_key, created_at, first_touched_at, last_touched_at, touch_count FROM jira_work_item_mappings WHERE execution_id = ? ORDER BY created_at, id`, executionID)
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
