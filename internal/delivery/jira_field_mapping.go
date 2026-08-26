package delivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
)

const StoryPointsFieldPurpose = "story_points"

type JiraFieldMapping struct {
	ID           string    `json:"id"`
	CloudID      string    `json:"cloud_id"`
	ProjectKey   string    `json:"project_key"`
	IssueTypeID  string    `json:"issue_type_id"`
	Purpose      string    `json:"purpose"`
	FieldID      string    `json:"field_id"`
	FieldName    string    `json:"field_name"`
	DiscoveredAt time.Time `json:"discovered_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (s *Store) GetJiraFieldMapping(ctx context.Context, cloudID, projectKey, issueTypeID, purpose string) (*JiraFieldMapping, error) {
	return scanJiraFieldMapping(s.db.Reader().QueryRowContext(ctx, `SELECT id, cloud_id, project_key, issue_type_id, purpose, field_id, field_name, discovered_at, updated_at FROM jira_field_mappings WHERE cloud_id = ? AND project_key = ? AND issue_type_id = ? AND purpose = ?`, cloudID, projectKey, issueTypeID, purpose))
}

func (s *Store) UpsertJiraFieldMapping(ctx context.Context, idempotencyKey, cloudID, projectKey, issueTypeID, purpose, fieldID, fieldName string) (*JiraFieldMapping, error) {
	values := []string{cloudID, projectKey, issueTypeID, purpose, fieldID, fieldName}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("delivery: Jira field mapping values are required")
		}
	}
	var out JiraFieldMapping
	now := time.Now().UTC()
	err := s.db.Write(ctx, idempotencyKey, "upsert Jira field mapping "+purpose, func(tx *sql.Tx) error {
		out = JiraFieldMapping{ID: newID(), CloudID: cloudID, ProjectKey: projectKey, IssueTypeID: issueTypeID, Purpose: purpose, FieldID: fieldID, FieldName: fieldName, DiscoveredAt: now, UpdatedAt: now}
		_, err := tx.ExecContext(ctx, `INSERT INTO jira_field_mappings (id, cloud_id, project_key, issue_type_id, purpose, field_id, field_name, discovered_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(cloud_id, project_key, issue_type_id, purpose) DO UPDATE SET field_id = excluded.field_id, field_name = excluded.field_name, discovered_at = excluded.discovered_at, updated_at = excluded.updated_at`, out.ID, out.CloudID, out.ProjectKey, out.IssueTypeID, out.Purpose, out.FieldID, out.FieldName, now.Format(timeLayout), now.Format(timeLayout))
		if err != nil {
			return err
		}
		return nil
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetJiraFieldMapping(ctx, cloudID, projectKey, issueTypeID, purpose)
	}
	if err != nil {
		return nil, err
	}
	return s.GetJiraFieldMapping(ctx, cloudID, projectKey, issueTypeID, purpose)
}

type jiraFieldMappingScanner interface{ Scan(...any) error }

func scanJiraFieldMapping(row jiraFieldMappingScanner) (*JiraFieldMapping, error) {
	var out JiraFieldMapping
	var discovered, updated string
	if err := row.Scan(&out.ID, &out.CloudID, &out.ProjectKey, &out.IssueTypeID, &out.Purpose, &out.FieldID, &out.FieldName, &discovered, &updated); err != nil {
		return nil, noRow(err)
	}
	var err error
	if out.DiscoveredAt, err = scanTime(discovered); err != nil {
		return nil, err
	}
	out.UpdatedAt, err = scanTime(updated)
	return &out, err
}
