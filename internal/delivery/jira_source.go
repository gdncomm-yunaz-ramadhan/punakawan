package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/deliveryhooks"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
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

// JiraWriteIntent is a row of the legacy jira_write_intents table.
// Nothing writes it any more: every Jira effect a delivery produces is now
// one durable outbox intent in provider_write_intents, enqueued by
// internal/jirahooks and executed by the daemon's worker pool. The type and
// its read path stay so a delivery recorded before that move still reports
// what it recorded, the same way the legacy usage ledger does.
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

// AssessJira records one judgement of how clear a Jira source is.
// Assessments are append-only: a delivery re-assessed after the question
// was answered keeps the earlier judgement alongside the newer one, which
// is what makes "it was unclear and then it was answered" readable later.
//
// A replayed idempotency key returns the assessment that key already
// recorded rather than an error, because this now runs on retryable paths
// - start_delivery records the clarity it was given, and a retried start
// must not fail on the assessment it already wrote.
func (s *Store) AssessJira(ctx context.Context, idempotencyKey, executionID, sessionID, snapshotID, clarity, rationale string) (*JiraAssessment, error) {
	if clarity != ClarityClear && clarity != ClarityNeedsClarification {
		return nil, fmt.Errorf("delivery: invalid Jira clarity")
	}
	if strings.TrimSpace(rationale) == "" {
		return nil, fmt.Errorf("delivery: assessment rationale is required")
	}
	var out JiraAssessment
	now := time.Now().UTC()
	recorded := false
	err := s.db.Write(ctx, idempotencyKey, "assess Jira delivery "+executionID, func(tx *sql.Tx) error {
		recorded = true
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
	if errors.Is(err, storage.ErrDuplicateWrite) || (err == nil && !recorded) {
		return s.getJiraAssessmentByKey(ctx, idempotencyKey, executionID)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// getJiraAssessmentByKey reads back the assessment a replayed
// idempotency key already recorded. jira_assessments does not store the
// key, so the newest assessment for that execution is the one that write
// produced.
func (s *Store) getJiraAssessmentByKey(ctx context.Context, idempotencyKey, executionID string) (*JiraAssessment, error) {
	_ = idempotencyKey
	return scanAssessment(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, execution_id, session_id, snapshot_id, clarity, rationale, assessed_at FROM jira_assessments WHERE execution_id = ? ORDER BY assessed_at DESC, id DESC LIMIT 1`, executionID))
}

// Jira requirement clarity, as the jira_assessments CHECK constraint
// defines it.
const (
	ClarityClear              = "clear"
	ClarityNeedsClarification = "needs_clarification"
)

// clarityQuestionPrefix marks an unresolved input that is a question
// about the requirement itself rather than a reference nothing could
// classify. Both live in the same list because both are "this delivery is
// waiting on an answer", which is what a reader of pending_questions
// wants to know.
const clarityQuestionPrefix = "clarity:"

// ClarityQuestionReference names the pending question recorded for one
// issue whose requirement was judged unclear.
func ClarityQuestionReference(issueKey string) string {
	return clarityQuestionPrefix + strings.ToUpper(strings.TrimSpace(issueKey))
}

// ClarityQuestionIssueKey is the issue key one of these references names.
func ClarityQuestionIssueKey(reference string) string {
	return strings.TrimPrefix(reference, clarityQuestionPrefix)
}

// IsClarityQuestion reports whether a pending question reference is one
// of these rather than an unclassifiable requirement reference.
func IsClarityQuestion(reference string) bool {
	return strings.HasPrefix(reference, clarityQuestionPrefix)
}

// OpenClarityQuestion records that this delivery is waiting on an answer
// about what its requirement means, and asks for it wherever the
// workspace projects delivery events - on the Jira issue itself, for a
// workspace with write-back on.
//
// Recording "needs clarification" used to do nothing beyond storing the
// judgement: no gate, no question, nothing on the issue. The person who
// wrote the requirement had no way to learn it was unclear, and the
// delivery looked exactly like one that was understood.
func (s *Store) OpenClarityQuestion(ctx context.Context, idempotencyKey, orchestrationID, issueKey, rationale string) error {
	orch, err := s.GetOrchestration(ctx, orchestrationID)
	if err != nil {
		return err
	}
	reference := ClarityQuestionReference(issueKey)
	for _, open := range orch.UnresolvedInputs {
		if open.Reference == reference {
			return nil
		}
	}
	note := strings.TrimSpace(rationale)
	input := protocol.DeliveryOrchestrationUnresolvedInputsElem{Reference: reference}
	if note != "" {
		input.Note = &note
	}
	if _, err := s.RegisterInput(ctx, idempotencyKey, orchestrationID, orch.Revision, input); err != nil {
		return err
	}
	s.dispatchOrchestrationEvent(ctx, orchestrationID, "", deliveryhooks.EventRequirementUnclear, note, nil)
	return nil
}

// CloseClarityQuestion clears the question opened for issueKey, for a
// caller that has just recorded an assessment answering it.
func (s *Store) CloseClarityQuestion(ctx context.Context, idempotencyKey, orchestrationID, issueKey string) error {
	return s.ResolvePendingInput(ctx, idempotencyKey, orchestrationID, ClarityQuestionReference(issueKey))
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
		expectedKey, err := CanonicalKey(SourceInput{Provider: "jira", ExternalID: issueKey})
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

// TouchJiraWorkItemForTask touches the Jira issue mapped to one task of
// this delivery, for a caller that knows which task it just finished but
// not which issue that task was bound to (complete_delivery_lane). A task
// with no mapping is ErrNotFound, exactly as touching an unmapped issue
// directly is: a touch enriches a durable mapping, it never invents one.
func (s *Store) TouchJiraWorkItemForTask(ctx context.Context, key, orchestrationID, parentTaskID, sessionID string, at time.Time) (*JiraWorkItemMapping, error) {
	var executionID, issueKey string
	if err := s.db.Reader().QueryRowContext(ctx, `SELECT execution_id, jira_issue_key FROM jira_work_item_mappings WHERE orchestration_id = ? AND parent_task_id = ? ORDER BY created_at, id LIMIT 1`, orchestrationID, parentTaskID).Scan(&executionID, &issueKey); err != nil {
		return nil, noRow(err)
	}
	return s.TouchJiraWorkItem(ctx, key, executionID, sessionID, issueKey, at)
}

// GetJiraSnapshot reads one captured Jira source snapshot by id.
func (s *Store) GetJiraSnapshot(ctx context.Context, id string) (*JiraSourceSnapshot, error) {
	return scanSnapshot(s.db.Reader().QueryRowContext(ctx, `SELECT id, case_id, execution_id, session_id, jira_issue_key, version, title, body, content_hash, captured_at FROM jira_source_snapshots WHERE id = ?`, id))
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
