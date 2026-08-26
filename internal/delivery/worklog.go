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

// WorkLogEntry is one measured interval of work attributed to a delivery lane
// and an explicit Jira task. Jira synchronization is a projection of this
// ledger entry; a failed external write never discards the recorded work.
type WorkLogEntry struct {
	ID              string     `json:"id"`
	OrchestrationID string     `json:"orchestration_id"`
	LaneID          string     `json:"lane_id"`
	ParentTaskID    string     `json:"parent_task_id,omitempty"`
	SessionID       string     `json:"session_id,omitempty"`
	JiraIssueKey    string     `json:"jira_issue_key"`
	StartedAt       time.Time  `json:"started_at"`
	DurationSeconds int        `json:"duration_seconds"`
	Summary         string     `json:"summary"`
	SyncStatus      string     `json:"sync_status"`
	JiraWorklogID   string     `json:"jira_worklog_id,omitempty"`
	SyncedAt        *time.Time `json:"synced_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// RecordWorkLog records actual elapsed work against one lane and Jira task.
// The Jira task is required rather than inferred from the delivery's first
// source: a delivery may span several Jira subtasks and work must never be
// silently attributed to the wrong one.
func (s *Store) RecordWorkLog(ctx context.Context, idempotencyKey, id, orchestrationID, laneID, sessionID, jiraIssueKey string, startedAt time.Time, durationSeconds int, summary string) (*WorkLogEntry, error) {
	jiraIssueKey = strings.TrimSpace(jiraIssueKey)
	summary = strings.TrimSpace(summary)
	if id == "" || orchestrationID == "" || laneID == "" || jiraIssueKey == "" || summary == "" {
		return nil, fmt.Errorf("delivery: worklog requires id, orchestration, lane, Jira issue, and summary")
	}
	if durationSeconds <= 0 {
		return nil, fmt.Errorf("delivery: worklog duration must be positive")
	}
	if startedAt.IsZero() {
		return nil, fmt.Errorf("delivery: worklog start time is required")
	}
	startedAt = startedAt.UTC()
	createdAt := time.Now().UTC()
	parentTaskID := ""

	err := s.db.Write(ctx, idempotencyKey, "record worklog "+id, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		orch, err := reduceOrchestration(orchestrationID, events)
		if err != nil {
			return err
		}
		if isTerminal(orch.Status) {
			return ErrInvalidState
		}
		lane, err := reduceLane(orchestrationID, laneID, events)
		if err != nil {
			return err
		}
		if lane.ParentTaskId != nil {
			parentTaskID = *lane.ParentTaskId
		}
		_, err = tx.ExecContext(ctx, `
            INSERT INTO delivery_worklogs
                (id, orchestration_id, lane_id, parent_task_id, session_id, jira_issue_key, started_at, duration_seconds, summary, created_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, orchestrationID, laneID, parentTaskID, strings.TrimSpace(sessionID), jiraIssueKey,
			startedAt.Format(timeLayout), durationSeconds, summary, createdAt.Format(timeLayout))
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(map[string]any{
			"lane_id": laneID, "parent_task_id": parentTaskID, "session_id": strings.TrimSpace(sessionID),
			"jira_issue_key": jiraIssueKey, "duration_seconds": durationSeconds, "summary": summary,
		})
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &id, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeWorklogRecorded), Payload: string(encoded),
			Sequence: len(events), OccurredAt: createdAt,
		})
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetWorkLog(ctx, orchestrationID, id)
	}
	if err != nil {
		return nil, fmt.Errorf("delivery: record worklog: %w", err)
	}
	entry, err := s.GetWorkLog(ctx, orchestrationID, id)
	if err != nil {
		return nil, err
	}
	s.dispatchWorkLogEvent(ctx, entry)
	return entry, nil
}

func (s *Store) GetWorkLog(ctx context.Context, orchestrationID, id string) (*WorkLogEntry, error) {
	row := s.db.Reader().QueryRowContext(ctx, `
        SELECT id, orchestration_id, lane_id, parent_task_id, session_id, jira_issue_key, started_at, duration_seconds,
               summary, sync_status, jira_worklog_id, synced_at, created_at
        FROM delivery_worklogs WHERE orchestration_id = ? AND id = ?`, orchestrationID, id)
	return scanWorkLog(row)
}

// ListWorkLogs returns the delivery's authoritative work ledger in recording
// order. The panel renders this data even while Jira synchronization is pending.
func (s *Store) ListWorkLogs(ctx context.Context, orchestrationID string) ([]WorkLogEntry, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
        SELECT id, orchestration_id, lane_id, parent_task_id, session_id, jira_issue_key, started_at, duration_seconds,
               summary, sync_status, jira_worklog_id, synced_at, created_at
        FROM delivery_worklogs WHERE orchestration_id = ? ORDER BY created_at, id`, orchestrationID)
	if err != nil {
		return nil, fmt.Errorf("delivery: list worklogs: %w", err)
	}
	defer rows.Close()
	out := make([]WorkLogEntry, 0)
	for rows.Next() {
		entry, err := scanWorkLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("delivery: list worklogs: %w", err)
	}
	return out, nil
}

// MarkWorkLogSynced records Jira's durable worklog identity after a successful
// adapter call. It is idempotent so hook retries do not alter accounting.
func (s *Store) MarkWorkLogSynced(ctx context.Context, orchestrationID, id, jiraWorklogID string) error {
	now := time.Now().UTC()
	_, err := s.db.Reader().ExecContext(ctx, `
        UPDATE delivery_worklogs SET sync_status = 'synced', jira_worklog_id = ?, synced_at = ?
        WHERE orchestration_id = ? AND id = ?`, strings.TrimSpace(jiraWorklogID), now.Format(timeLayout), orchestrationID, id)
	if err != nil {
		return fmt.Errorf("delivery: mark worklog synced: %w", err)
	}
	return nil
}

// requireLaneWorkLog applies the opt-in completion policy only to deliveries
// actually linked to Jira. Non-Jira work keeps its existing lifecycle.
func (s *Store) requireLaneWorkLog(ctx context.Context, orchestrationID, laneID string) error {
	if !s.requireJiraWorkLogs {
		return nil
	}
	sources, err := s.ListRequirementSources(ctx, orchestrationID)
	if err != nil {
		return err
	}
	jiraBacked := false
	for _, source := range sources {
		if source.Provider == protocol.RequirementSourceProviderJira {
			jiraBacked = true
			break
		}
	}
	if !jiraBacked {
		return nil
	}
	var count int
	if err := s.db.Reader().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM delivery_worklogs WHERE orchestration_id = ? AND lane_id = ?`, orchestrationID, laneID).Scan(&count); err != nil {
		return fmt.Errorf("delivery: check lane worklogs: %w", err)
	}
	if count == 0 {
		return ErrWorkLogRequired
	}
	return nil
}

type workLogScanner interface{ Scan(...any) error }

func scanWorkLog(row workLogScanner) (*WorkLogEntry, error) {
	var entry WorkLogEntry
	var startedAt, createdAt string
	var syncedAt sql.NullString
	if err := row.Scan(&entry.ID, &entry.OrchestrationID, &entry.LaneID, &entry.ParentTaskID, &entry.SessionID,
		&entry.JiraIssueKey, &startedAt, &entry.DurationSeconds, &entry.Summary, &entry.SyncStatus,
		&entry.JiraWorklogID, &syncedAt, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("delivery: scan worklog: %w", err)
	}
	var err error
	if entry.StartedAt, err = time.Parse(timeLayout, startedAt); err != nil {
		return nil, fmt.Errorf("delivery: parse worklog start: %w", err)
	}
	if entry.CreatedAt, err = time.Parse(timeLayout, createdAt); err != nil {
		return nil, fmt.Errorf("delivery: parse worklog creation: %w", err)
	}
	if syncedAt.Valid {
		t, err := time.Parse(timeLayout, syncedAt.String)
		if err != nil {
			return nil, fmt.Errorf("delivery: parse worklog sync: %w", err)
		}
		entry.SyncedAt = &t
	}
	return &entry, nil
}

func (s *Store) dispatchWorkLogEvent(ctx context.Context, entry *WorkLogEntry) {
	if s.hooks == nil {
		return
	}
	orch, err := s.GetOrchestration(ctx, entry.OrchestrationID)
	if err != nil {
		return
	}
	s.hooks.Dispatch(ctx, deliveryhooks.Event{
		Type: deliveryhooks.EventWorkLogged, DeliveryID: entry.OrchestrationID, EntityID: entry.ID,
		Revision: orch.Revision, Title: derefOrEmpty(orch.Title), Projects: orch.ProjectIds,
		JiraIssueKey: entry.JiraIssueKey, DurationSeconds: entry.DurationSeconds, Summary: entry.Summary,
	})
}
