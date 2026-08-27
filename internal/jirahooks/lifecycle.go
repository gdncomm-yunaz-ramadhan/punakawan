package jirahooks

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// Lifecycle hydrates a delivery's Jira source and projects its durable Jira
// write intents. It never invokes a Jira write without a pre-existing intent.
type Lifecycle struct {
	store    *delivery.Store
	registry gateResolver
}

func NewLifecycle(store *delivery.Store, registry gateResolver) *Lifecycle {
	return &Lifecycle{store: store, registry: registry}
}

// Hydrate reads the delivery case's exact Jira issue through the configured
// adapter and records the returned title and description as an immutable
// snapshot. The idempotency key is owned by the caller so repeated MCP calls
// return the snapshot from the original read rather than silently replacing it.
func (l *Lifecycle) Hydrate(ctx context.Context, executionID, sessionID, idempotencyKey string) (*delivery.JiraSourceSnapshot, error) {
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
		return nil, fmt.Errorf("jirahooks: hydrate Jira issue %s: %w", lifecycle.Case.JiraIssueKey, err)
	}
	var result struct {
		Normalized struct {
			Summary     string `json:"summary"`
			Description string `json:"description"`
			Subtasks    []struct {
				Key     string `json:"key"`
				Summary string `json:"summary"`
			} `json:"subtasks"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("jirahooks: decode Jira issue %s: %w", lifecycle.Case.JiraIssueKey, err)
	}
	var body strings.Builder
	body.WriteString(result.Normalized.Description)
	for _, subtask := range result.Normalized.Subtasks {
		if strings.TrimSpace(subtask.Key) == "" {
			continue
		}
		raw, err := gate.Call(ctx, lifecycle.Case.ID, "atlassian.getJiraIssue", map[string]any{"issueIdOrKey": subtask.Key})
		if err != nil {
			return nil, fmt.Errorf("jirahooks: hydrate Jira subtask %s: %w", subtask.Key, err)
		}
		var child struct {
			Normalized struct {
				Summary     string `json:"summary"`
				Description string `json:"description"`
			} `json:"normalized"`
		}
		if err := json.Unmarshal(raw, &child); err != nil {
			return nil, fmt.Errorf("jirahooks: decode Jira subtask %s: %w", subtask.Key, err)
		}
		fmt.Fprintf(&body, "\n\n## Subtask %s: %s\n%s", subtask.Key, child.Normalized.Summary, child.Normalized.Description)
	}
	return l.store.CaptureJiraSnapshot(ctx, idempotencyKey, executionID, sessionID, result.Normalized.Summary, body.String())
}

// Execute applies one pending Jira write intent. A successful intent is never
// called again, so an explicit retry cannot duplicate an already-recorded
// external mutation.
func (l *Lifecycle) Execute(ctx context.Context, intentID, resolutionKey string) (*delivery.JiraWriteIntent, error) {
	intent, err := l.store.GetJiraWriteIntent(ctx, intentID)
	if err != nil {
		return nil, fmt.Errorf("jirahooks: get Jira write intent: %w", err)
	}
	if intent.Status == "succeeded" {
		return intent, nil
	}
	if intent.Status != "pending" && intent.Status != "retrying" {
		return nil, fmt.Errorf("jirahooks: Jira write intent %s is %q, not pending or retrying", intent.ID, intent.Status)
	}
	attemptKey := resolutionKey + ":" + strconv.Itoa(intent.AttemptCount+1)
	op, params, err := adapterWrite(intent)
	if err != nil {
		return nil, err
	}
	gate, err := l.registry.Gate(ctx, "atlassian")
	if err != nil {
		return l.resolveFailure(ctx, intent, attemptKey, fmt.Errorf("open atlassian adapter: %w", err))
	}
	runID := "jira-execution-" + intent.ExecutionID
	if gate.RequiresApproval(op) {
		if _, err := gate.RequestApproval(runID, op, protocol.ApprovalRecordRequestedBySemar, adapters.BuildApprovalPreview(op, params)); err != nil {
			return l.resolveFailure(ctx, intent, attemptKey, fmt.Errorf("request adapter approval: %w", err))
		}

	}
	raw, err := gate.Call(ctx, runID, op, params)
	if err != nil {
		return l.resolveFailure(ctx, intent, attemptKey, err)
	}
	externalID, err := externalID(intent.Action, raw)
	if err != nil {
		return l.resolveFailure(ctx, intent, attemptKey, fmt.Errorf("decode adapter result: %w", err))
	}
	resolved, err := l.store.ResolveJiraWriteIntent(ctx, attemptKey, intent.ID, externalID, "", nil)
	if err != nil {
		return nil, fmt.Errorf("jirahooks: resolve successful Jira write intent: %w", err)
	}
	if intent.Action == "worklog" {
		if worklogID, _ := stringPayload(intent.Payload, "worklog_id"); worklogID != "" {
			execution, err := l.store.GetExecution(ctx, intent.ExecutionID)
			if err != nil {
				return nil, fmt.Errorf("jirahooks: get worklog execution: %w", err)
			}
			if err := l.store.MarkWorkLogSynced(ctx, execution.OrchestrationID, worklogID, externalID); err != nil {
				return nil, fmt.Errorf("jirahooks: mark worklog synced: %w", err)
			}
		}
	}
	return resolved, nil
}

// ApproveWrites records one explicit human approval for every pending Jira
// write in this delivery execution. Gate approvals are scoped to the execution
// run id, so subsequent comment, estimate, and story-point writes reuse it.
func (l *Lifecycle) ApproveWrites(ctx context.Context, executionID, approvedBy string) error {
	execution, err := l.store.GetExecution(ctx, executionID)
	if err != nil {
		return fmt.Errorf("jirahooks: get delivery execution: %w", err)
	}
	lifecycle, err := l.store.GetDeliveryLifecycle(ctx, execution.OrchestrationID)
	if err != nil {
		return fmt.Errorf("jirahooks: get delivery lifecycle: %w", err)
	}
	var intent *delivery.JiraWriteIntent
	for i := range lifecycle.WriteIntents {
		if lifecycle.WriteIntents[i].Status == "pending" || lifecycle.WriteIntents[i].Status == "retrying" {
			intent = &lifecycle.WriteIntents[i]
			break
		}
	}
	if intent == nil {
		return fmt.Errorf("jirahooks: delivery execution %s has no pending Jira writes", executionID)
	}
	op, params, err := adapterWrite(intent)
	if err != nil {
		return err
	}
	gate, err := l.registry.Gate(ctx, "atlassian")
	if err != nil {
		return fmt.Errorf("jirahooks: open atlassian adapter: %w", err)
	}
	runID := "jira-execution-" + executionID
	if gate.RequiresApproval(op) {
		if _, err := gate.RequestApproval(runID, op, protocol.ApprovalRecordRequestedBySemar, adapters.BuildApprovalPreview(op, params)); err != nil {
			return fmt.Errorf("jirahooks: request adapter approval: %w", err)
		}
		if err := gate.Approve(runID, approvedBy); err != nil {
			return fmt.Errorf("jirahooks: approve adapter writes: %w", err)
		}
	}
	return nil
}

// ExecutePending applies every pending/retrying intent in creation order for
// one execution. A failed intent is durably scheduled for retry; later,
// independent intents are still attempted.
func (l *Lifecycle) ExecutePending(ctx context.Context, executionID, resolutionKeyPrefix string) ([]delivery.JiraWriteIntent, error) {
	execution, err := l.store.GetExecution(ctx, executionID)
	if err != nil {
		return nil, fmt.Errorf("jirahooks: get delivery execution: %w", err)
	}
	lifecycle, err := l.store.GetDeliveryLifecycle(ctx, execution.OrchestrationID)
	if err != nil {
		return nil, fmt.Errorf("jirahooks: get delivery lifecycle: %w", err)
	}
	out := make([]delivery.JiraWriteIntent, 0, len(lifecycle.WriteIntents))
	var firstErr error
	for _, intent := range lifecycle.WriteIntents {
		if intent.Status != "pending" && intent.Status != "retrying" {
			continue
		}
		resolved, err := l.Execute(ctx, intent.ID, resolutionKeyPrefix+":"+intent.ID)
		if resolved != nil {
			out = append(out, *resolved)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return out, firstErr
}

func (l *Lifecycle) resolveFailure(ctx context.Context, intent *delivery.JiraWriteIntent, resolutionKey string, failure error) (*delivery.JiraWriteIntent, error) {
	retryAt := time.Now().UTC().Add(time.Minute)
	resolved, err := l.store.ResolveJiraWriteIntent(ctx, resolutionKey, intent.ID, "", failure.Error(), &retryAt)
	if err != nil {
		return nil, fmt.Errorf("jirahooks: resolve failed Jira write intent: %w", err)
	}
	return resolved, fmt.Errorf("jirahooks: execute Jira write intent %s: %w", intent.ID, failure)
}

func adapterWrite(intent *delivery.JiraWriteIntent) (string, map[string]any, error) {
	switch intent.Action {
	case "add_comment", "comment", "clarification_comment":
		body, ok := payloadString(intent.Payload, "comment_body", "comment", "body")
		if !ok || body == "" {
			return "", nil, fmt.Errorf("jirahooks: %s intent requires payload.comment_body", intent.Action)
		}
		return "atlassian.addJiraComment", map[string]any{"issueIdOrKey": intent.JiraIssueKey, "commentBody": body}, nil
	case "update_description", "description":
		description, ok := payloadString(intent.Payload, "description", "description_body")
		if !ok {
			return "", nil, fmt.Errorf("jirahooks: %s intent requires payload.description", intent.Action)
		}
		return "atlassian.editJiraIssue", map[string]any{"issueIdOrKey": intent.JiraIssueKey, "description": description}, nil
	case "transition_status", "transition":
		transitionID, ok := payloadString(intent.Payload, "transition_id", "transitionId")
		if !ok {
			return "", nil, fmt.Errorf("jirahooks: %s intent requires payload.transition_id", intent.Action)
		}
		return "atlassian.transitionJiraIssue", map[string]any{"issueIdOrKey": intent.JiraIssueKey, "transitionId": transitionID}, nil
	case "create_subtask":
		projectKey, projectOK := payloadString(intent.Payload, "project_key", "projectKey")
		issueTypeName, typeOK := payloadString(intent.Payload, "issue_type_name", "issueTypeName")
		candidates, candidatesOK := payloadSlice(intent.Payload, "candidates")
		if !projectOK || !typeOK || !candidatesOK || len(candidates) == 0 {
			return "", nil, fmt.Errorf("jirahooks: create_subtask intent requires project_key, issue_type_name, and candidates")
		}
		return "atlassian.createJiraSubtask", map[string]any{"parentKey": intent.JiraIssueKey, "projectKey": projectKey, "issueTypeName": issueTypeName, "candidates": candidates}, nil
	case "update_estimate":
		original, hasOriginal := payloadString(intent.Payload, "original_estimate", "originalEstimate")
		remaining, hasRemaining := payloadString(intent.Payload, "remaining_estimate", "remainingEstimate")
		if !hasOriginal && !hasRemaining {
			return "", nil, fmt.Errorf("jirahooks: update_estimate intent requires original_estimate or remaining_estimate")
		}
		params := map[string]any{"issueIdOrKey": intent.JiraIssueKey}
		if hasOriginal {
			params["originalEstimate"] = original
		}
		if hasRemaining {
			params["remainingEstimate"] = remaining
		}
		return "atlassian.editJiraIssue", params, nil
	case "update_story_points":
		points, ok := payloadNumber(intent.Payload, "story_points", "storyPoints")
		fieldID, fieldOK := storyPointsFieldID(intent.Payload)
		if !ok || !fieldOK || strings.TrimSpace(fieldID) == "" {
			return "", nil, fmt.Errorf("jirahooks: update_story_points intent requires numeric story_points and discovered field metadata")
		}
		return "atlassian.editJiraIssue", map[string]any{"issueIdOrKey": intent.JiraIssueKey, "storyPoints": points, "storyPointsFieldId": fieldID}, nil
	case "worklog":
		seconds, ok := payloadNumber(intent.Payload, "time_spent_seconds", "timeSpentSeconds")
		if !ok || seconds <= 0 || seconds != float64(int(seconds)) {
			return "", nil, fmt.Errorf("jirahooks: worklog intent requires positive integral time_spent_seconds")
		}
		params := map[string]any{"issueIdOrKey": intent.JiraIssueKey, "timeSpentSeconds": int(seconds)}
		if comment, ok := stringPayload(intent.Payload, "comment"); ok {
			params["comment"] = comment
		}
		return "atlassian.addWorklog", params, nil
	default:
		return "", nil, fmt.Errorf("jirahooks: unsupported Jira write action %q", intent.Action)
	}
}

func stringPayload(payload map[string]any, key string) (string, bool) {
	value, ok := payload[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return strings.TrimSpace(text), ok
}

func payloadString(payload map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := stringPayload(payload, key); ok {
			return value, true
		}
	}
	return "", false
}

func payloadNumber(payload map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		if value, ok := numberPayload(payload, key); ok {
			return value, true
		}
	}
	return 0, false
}

func payloadSlice(payload map[string]any, key string) ([]any, bool) {
	value, ok := payload[key]
	if !ok {
		return nil, false
	}
	if values, ok := value.([]any); ok {
		return values, true
	}
	if values, ok := value.([]map[string]any); ok {
		out := make([]any, len(values))
		for i := range values {
			out[i] = values[i]
		}
		return out, true
	}
	return nil, false
}

func numberPayload(payload map[string]any, key string) (float64, bool) {
	value, ok := payload[key]
	if !ok {
		return 0, false
	}
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func storyPointsFieldID(payload map[string]any) (string, bool) {
	if id, ok := payloadString(payload, "story_points_field_id", "storyPointsFieldId"); ok {
		return id, true
	}
	metadata, ok := payload["field_metadata"].(map[string]any)
	if !ok {
		return "", false
	}
	return stringPayload(metadata, "id")
}

func externalID(action string, raw json.RawMessage) (string, error) {
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	for _, key := range []string{"commentId", "worklogId"} {
		if id, ok := stringPayload(result, key); ok {
			return id, nil
		}
	}
	if action == "create_subtask" {
		if created, ok := result["created"].([]any); ok && len(created) > 0 {
			if first, ok := created[0].(map[string]any); ok {
				if key, ok := stringPayload(first, "key"); ok {
					return key, nil
				}
			}
		}
		if skipped, ok := result["skipped"].([]any); ok && len(skipped) > 0 {
			if first, ok := skipped[0].(map[string]any); ok {
				if key, ok := stringPayload(first, "existingKey"); ok {
					return key, nil
				}
			}
		}
	}
	if issueKey, ok := stringPayload(result, "issueIdOrKey"); ok {
		return issueKey, nil
	}
	return "", nil
}
