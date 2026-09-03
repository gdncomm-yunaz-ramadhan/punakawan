package jirahooks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ygrip/punakawan/internal/jiraworkflow"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// SubtaskEstimate is one Jira subtask of a delivery's parent issue, with
// its story points and hour estimate filled in where resolvable. A field
// left nil (never a fabricated zero) means that piece of data could not
// be resolved - see SuggestSubtaskBreakdown's doc comment for the ways
// that can happen.
type SubtaskEstimate struct {
	RequirementSourceId string   `json:"requirement_source_id"`
	IssueKey             string   `json:"issue_key"`
	Title                string   `json:"title"`
	StoryPoints          *float64 `json:"story_points,omitempty"`
	EstimatedHours       *float64 `json:"estimated_hours,omitempty"`
}

// SuggestSubtaskBreakdown builds a best-effort per-subtask story-point and
// hour estimate for executionID's delivery, read fresh from Jira. It never
// returns an error: any lookup failure along the way degrades to a shorter
// result (as far as it got) plus a note explaining what could not be
// resolved, matching the rest of this package's best-effort conventions
// (see touchJiraIssue in internal/mcpserver). Nothing here persists
// anything - this is a read for display, not a durable record.
func (l *Lifecycle) SuggestSubtaskBreakdown(ctx context.Context, executionID, idempotencyKey string, cfg *jiraworkflow.Config) ([]SubtaskEstimate, string) {
	execution, err := l.store.GetExecution(ctx, executionID)
	if err != nil {
		return nil, ""
	}
	lifecycle, err := l.store.GetDeliveryLifecycle(ctx, execution.OrchestrationID)
	if err != nil {
		return nil, ""
	}

	sources, err := l.store.ListRequirementSources(ctx, execution.OrchestrationID)
	if err != nil {
		slog.Warn("jirahooks: list requirement sources for subtask breakdown", "orchestration_id", execution.OrchestrationID, "error", err)
		return nil, ""
	}

	var parent *protocol.RequirementSource
	for _, source := range sources {
		if source == nil || source.ParentSourceId != nil || source.ExternalId == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(*source.ExternalId), lifecycle.Case.JiraIssueKey) {
			parent = source
			break
		}
	}
	if parent == nil {
		return nil, ""
	}

	var estimates []SubtaskEstimate
	for _, source := range sources {
		if source == nil || source.ParentSourceId == nil || *source.ParentSourceId != parent.Id {
			continue
		}
		issueKey := ""
		if source.ExternalId != nil {
			issueKey = *source.ExternalId
		}
		estimates = append(estimates, SubtaskEstimate{
			RequirementSourceId: source.Id,
			IssueKey:            issueKey,
			Title:               source.Title,
		})
	}
	if len(estimates) == 0 {
		return nil, ""
	}

	mapping, err := l.ResolveStoryPointsField(ctx, executionID, idempotencyKey, false)
	if err != nil {
		return estimates, "no Story Points field could be resolved for this project/issue type; showing subtasks without an estimate"
	}

	gate, err := l.registry.Gate(ctx, jiraAdapterID(lifecycle.Case.SourceTenant))
	if err != nil {
		return estimates, "could not read subtask story points from Jira; showing subtasks without an estimate"
	}
	var issueKeys []string
	for _, estimate := range estimates {
		if strings.TrimSpace(estimate.IssueKey) != "" {
			issueKeys = append(issueKeys, estimate.IssueKey)
		}
	}
	if len(issueKeys) == 0 {
		return estimates, "could not read subtask story points from Jira; showing subtasks without an estimate"
	}
	raw, err := gate.Call(ctx, lifecycle.Case.ID, "atlassian.searchJira", map[string]any{
		"jql":    fmt.Sprintf("key in (%s)", strings.Join(issueKeys, ",")),
		"fields": []string{"summary", mapping.FieldID},
	})
	if err != nil {
		return estimates, "could not read subtask story points from Jira; showing subtasks without an estimate"
	}

	var result struct {
		Normalized []struct {
			Key          string         `json:"key"`
			CustomFields map[string]any `json:"customFields"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return estimates, "could not read subtask story points from Jira; showing subtasks without an estimate"
	}

	pointsByKey := map[string]float64{}
	for _, issue := range result.Normalized {
		value, ok := issue.CustomFields[mapping.FieldID].(float64)
		if !ok {
			continue
		}
		pointsByKey[strings.ToUpper(issue.Key)] = value
	}

	var missingHours int
	for i := range estimates {
		points, ok := pointsByKey[strings.ToUpper(estimates[i].IssueKey)]
		if !ok {
			continue
		}
		estimates[i].StoryPoints = &points
		if cfg == nil {
			missingHours++
			continue
		}
		if hours, ok := cfg.EstimateHours(points); ok {
			estimates[i].EstimatedHours = &hours
		} else {
			missingHours++
		}
	}

	note := ""
	if missingHours > 0 {
		note = fmt.Sprintf("%d subtask(s) have story points but no points_to_hours ratio is configured", missingHours)
	}
	return estimates, note
}
