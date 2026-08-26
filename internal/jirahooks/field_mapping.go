package jirahooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/ygrip/punakawan/internal/delivery"
)

// ResolveStoryPointsField returns a cached Jira story-point field mapping, or
// discovers and persists the field advertised by Jira for this issue's exact
// project and issue type. Set refresh after an administrator changes fields.
func (l *Lifecycle) ResolveStoryPointsField(ctx context.Context, executionID, idempotencyKey string, refresh bool) (*delivery.JiraFieldMapping, error) {
	execution, err := l.store.GetExecution(ctx, executionID)
	if err != nil {
		return nil, fmt.Errorf("jirahooks: get delivery execution: %w", err)
	}
	lifecycle, err := l.store.GetDeliveryLifecycle(ctx, execution.OrchestrationID)
	if err != nil {
		return nil, fmt.Errorf("jirahooks: get delivery lifecycle: %w", err)
	}
	gate, err := l.registry.Gate(ctx, "atlassian")
	if err != nil {
		return nil, fmt.Errorf("jirahooks: open atlassian adapter: %w", err)
	}
	raw, err := gate.Call(ctx, lifecycle.Case.ID, "atlassian.getJiraIssue", map[string]any{"issueIdOrKey": lifecycle.Case.JiraIssueKey})
	if err != nil {
		return nil, fmt.Errorf("jirahooks: get Jira issue %s: %w", lifecycle.Case.JiraIssueKey, err)
	}
	var issue struct {
		Normalized struct {
			Source struct {
				URI string `json:"uri"`
			} `json:"source"`
			ProjectKey  string `json:"projectKey"`
			IssueTypeID string `json:"issueTypeId"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &issue); err != nil {
		return nil, fmt.Errorf("jirahooks: decode Jira issue %s: %w", lifecycle.Case.JiraIssueKey, err)
	}
	cloudID, err := jiraCloudID(issue.Normalized.Source.URI)
	if err != nil || issue.Normalized.ProjectKey == "" || issue.Normalized.IssueTypeID == "" {
		return nil, fmt.Errorf("jirahooks: Jira issue %s lacks cloud, project, or issue-type metadata", lifecycle.Case.JiraIssueKey)
	}
	if !refresh {
		mapping, err := l.store.GetJiraFieldMapping(ctx, cloudID, issue.Normalized.ProjectKey, issue.Normalized.IssueTypeID, delivery.StoryPointsFieldPurpose)
		if err == nil {
			return mapping, nil
		}
		if !errors.Is(err, delivery.ErrNotFound) {
			return nil, fmt.Errorf("jirahooks: read Jira story-point mapping: %w", err)
		}
	}
	raw, err = gate.Call(ctx, lifecycle.Case.ID, "atlassian.getIssueTypeFieldMeta", map[string]any{"projectIdOrKey": issue.Normalized.ProjectKey, "issueTypeId": issue.Normalized.IssueTypeID})
	if err != nil {
		return nil, fmt.Errorf("jirahooks: get Jira field metadata: %w", err)
	}
	var metadata struct {
		Payload struct {
			Fields map[string]struct {
				Name string `json:"name"`
			} `json:"fields"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, fmt.Errorf("jirahooks: decode Jira field metadata: %w", err)
	}
	for id, field := range metadata.Payload.Fields {
		if strings.EqualFold(strings.TrimSpace(field.Name), "Story Points") {
			return l.store.UpsertJiraFieldMapping(ctx, idempotencyKey, cloudID, issue.Normalized.ProjectKey, issue.Normalized.IssueTypeID, delivery.StoryPointsFieldPurpose, id, field.Name)
		}
	}
	return nil, fmt.Errorf("jirahooks: Jira issue %s has no Story Points field", lifecycle.Case.JiraIssueKey)
}

func jiraCloudID(sourceURI string) (string, error) {
	u, err := url.Parse(sourceURI)
	if err != nil || u.Scheme != "jira" || u.Host == "" {
		return "", fmt.Errorf("invalid Jira source URI %q", sourceURI)
	}
	return u.Host, nil
}
