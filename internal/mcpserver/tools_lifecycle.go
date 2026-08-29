package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/jirahooks"
)

type StartDeliverySessionInput struct {
	ExecutionID    string `json:"execution_id"`
	Participant    string `json:"participant"`
	ResumedFromID  string `json:"resumed_from_id,omitempty"`
	WorktreePath   string `json:"worktree_path,omitempty"`
	Provider       string `json:"provider,omitempty"`
	ID             string `json:"id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}
type DeliverySessionOutput struct {
	Session delivery.DeliverySession `json:"session"`
	View    delivery.DeliveryView    `json:"view"`
}

func startDeliverySessionHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, StartDeliverySessionInput) (*mcp.CallToolResult, DeliverySessionOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in StartDeliverySessionInput) (*mcp.CallToolResult, DeliverySessionOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, DeliverySessionOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		session, err := store.StartSession(ctx, key, in.ExecutionID, in.ID, in.Participant, in.ResumedFromID, in.WorktreePath, in.Provider)
		if err != nil {
			return nil, DeliverySessionOutput{}, fmt.Errorf("mcpserver: start delivery session: %w", err)
		}
		writeSessionMarker(session)
		view, err := store.BuildDeliveryView(ctx, session.OrchestrationID)
		if err != nil {
			return nil, DeliverySessionOutput{}, err
		}
		return nil, DeliverySessionOutput{Session: *session, View: *view}, nil
	}
}

type CheckpointDeliverySessionInput struct {
	SessionID       string   `json:"session_id"`
	Summary         string   `json:"summary"`
	ProgressPercent *float64 `json:"progress_percent,omitempty"`
	HandoffTo       string   `json:"handoff_to,omitempty"`
	ID              string   `json:"id,omitempty"`
	IdempotencyKey  string   `json:"idempotency_key,omitempty"`
}
type CheckpointDeliverySessionOutput struct {
	Checkpoint delivery.SessionCheckpoint `json:"checkpoint"`
	View       delivery.DeliveryView      `json:"view"`
}

func checkpointDeliverySessionHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CheckpointDeliverySessionInput) (*mcp.CallToolResult, CheckpointDeliverySessionOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CheckpointDeliverySessionInput) (*mcp.CallToolResult, CheckpointDeliverySessionOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, CheckpointDeliverySessionOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		checkpoint, err := store.CheckpointSession(ctx, key, in.SessionID, in.ID, in.Summary, in.ProgressPercent, in.HandoffTo)
		if err != nil {
			return nil, CheckpointDeliverySessionOutput{}, fmt.Errorf("mcpserver: checkpoint delivery session: %w", err)
		}
		session, err := store.GetSession(ctx, in.SessionID)
		if err != nil {
			return nil, CheckpointDeliverySessionOutput{}, err
		}
		view, err := store.BuildDeliveryView(ctx, session.OrchestrationID)
		if err != nil {
			return nil, CheckpointDeliverySessionOutput{}, err
		}
		return nil, CheckpointDeliverySessionOutput{Checkpoint: *checkpoint, View: *view}, nil
	}
}

type ReportDeliveryUsageInput struct {
	SessionID      string   `json:"session_id"`
	Kind           string   `json:"kind"`
	Category       string   `json:"category"`
	Model          string   `json:"model,omitempty"`
	Quantity       float64  `json:"quantity"`
	Unit           string   `json:"unit"`
	UnitPrice      *float64 `json:"unit_price,omitempty"`
	Currency       string   `json:"currency,omitempty"`
	PriceSource    string   `json:"price_source,omitempty"`
	ID             string   `json:"id,omitempty"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
	CorrectPrice   bool     `json:"correct_price,omitempty" jsonschema:"set only to enrich or clear price metadata for existing id; observed usage fields stay unchanged"`
}
type ReportDeliveryUsageOutput struct {
	Usage delivery.UsageEntry   `json:"usage"`
	View  delivery.DeliveryView `json:"view"`
}

func reportDeliveryUsageHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ReportDeliveryUsageInput) (*mcp.CallToolResult, ReportDeliveryUsageOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ReportDeliveryUsageInput) (*mcp.CallToolResult, ReportDeliveryUsageOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, ReportDeliveryUsageOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		var usage *delivery.UsageEntry
		if in.CorrectPrice {
			usage, err = store.CorrectUsagePricing(ctx, key, in.SessionID, in.ID, in.UnitPrice, in.Currency, in.PriceSource)
		} else {
			usage, err = store.RecordUsage(ctx, key, in.SessionID, in.ID, in.Kind, in.Category, in.Model, in.Quantity, in.Unit, in.UnitPrice, in.Currency, in.PriceSource)
		}
		if err != nil {
			return nil, ReportDeliveryUsageOutput{}, fmt.Errorf("mcpserver: report delivery usage: %w", err)
		}
		session, err := store.GetSession(ctx, in.SessionID)
		if err != nil {
			return nil, ReportDeliveryUsageOutput{}, err
		}
		view, err := store.BuildDeliveryView(ctx, session.OrchestrationID)
		if err != nil {
			return nil, ReportDeliveryUsageOutput{}, err
		}
		return nil, ReportDeliveryUsageOutput{Usage: *usage, View: *view}, nil
	}
}

type ReportDeliveryProgressInput struct {
	SessionID       string   `json:"session_id"`
	Summary         string   `json:"summary"`
	ProgressPercent *float64 `json:"progress_percent,omitempty"`
	ID              string   `json:"id,omitempty"`
	IdempotencyKey  string   `json:"idempotency_key,omitempty"`
}
type ReportDeliveryProgressOutput struct {
	Progress delivery.ProgressReport `json:"progress"`
	View     delivery.DeliveryView   `json:"view"`
}

func reportDeliveryProgressHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ReportDeliveryProgressInput) (*mcp.CallToolResult, ReportDeliveryProgressOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ReportDeliveryProgressInput) (*mcp.CallToolResult, ReportDeliveryProgressOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, ReportDeliveryProgressOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		progress, err := store.ReportProgress(ctx, key, in.SessionID, in.ID, in.Summary, in.ProgressPercent)
		if err != nil {
			return nil, ReportDeliveryProgressOutput{}, fmt.Errorf("mcpserver: report delivery progress: %w", err)
		}
		session, err := store.GetSession(ctx, in.SessionID)
		if err != nil {
			return nil, ReportDeliveryProgressOutput{}, err
		}
		view, err := store.BuildDeliveryView(ctx, session.OrchestrationID)
		if err != nil {
			return nil, ReportDeliveryProgressOutput{}, err
		}
		return nil, ReportDeliveryProgressOutput{Progress: *progress, View: *view}, nil
	}
}

type AssessJiraDeliveryInput struct {
	ExecutionID    string `json:"execution_id"`
	SessionID      string `json:"session_id,omitempty"`
	SnapshotID     string `json:"snapshot_id,omitempty"`
	SnapshotTitle  string `json:"snapshot_title,omitempty"`
	SnapshotBody   string `json:"snapshot_body,omitempty"`
	Clarity        string `json:"clarity"`
	Rationale      string `json:"rationale"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}
type AssessJiraDeliveryOutput struct {
	Assessment delivery.JiraAssessment `json:"assessment"`
	View       delivery.DeliveryView   `json:"view"`
}

func assessJiraDeliveryHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, AssessJiraDeliveryInput) (*mcp.CallToolResult, AssessJiraDeliveryOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in AssessJiraDeliveryInput) (*mcp.CallToolResult, AssessJiraDeliveryOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, AssessJiraDeliveryOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		snapshotID := in.SnapshotID
		if snapshotID == "" && (in.SnapshotTitle != "" || in.SnapshotBody != "") {
			snapshot, err := store.CaptureJiraSnapshot(ctx, key+":snapshot", in.ExecutionID, in.SessionID, in.SnapshotTitle, in.SnapshotBody)
			if err != nil {
				return nil, AssessJiraDeliveryOutput{}, fmt.Errorf("mcpserver: capture Jira snapshot: %w", err)
			}
			snapshotID = snapshot.ID
		}
		assessment, err := store.AssessJira(ctx, key, in.ExecutionID, in.SessionID, snapshotID, in.Clarity, in.Rationale)
		if err != nil {
			return nil, AssessJiraDeliveryOutput{}, fmt.Errorf("mcpserver: assess Jira delivery: %w", err)
		}
		execution, err := store.GetExecutionByCase(ctx, assessment.CaseID)
		if err != nil {
			return nil, AssessJiraDeliveryOutput{}, err
		}
		view, err := store.BuildDeliveryView(ctx, execution.OrchestrationID)
		if err != nil {
			return nil, AssessJiraDeliveryOutput{}, err
		}
		return nil, AssessJiraDeliveryOutput{Assessment: *assessment, View: *view}, nil
	}
}

type QueueJiraWriteInput struct {
	ExecutionID             string         `json:"execution_id"`
	SessionID               string         `json:"session_id,omitempty"`
	JiraIssueKey            string         `json:"jira_issue_key"`
	Action                  string         `json:"action"`
	RefreshStoryPointsField bool           `json:"refresh_story_points_field,omitempty"`
	Payload                 map[string]any `json:"payload,omitempty"`
	IdempotencyKey          string         `json:"idempotency_key,omitempty"`
}
type QueueJiraWriteOutput struct {
	Intent delivery.JiraWriteIntent `json:"intent"`
	View   delivery.DeliveryView    `json:"view"`
}

func queueJiraWriteHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, QueueJiraWriteInput) (*mcp.CallToolResult, QueueJiraWriteOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in QueueJiraWriteInput) (*mcp.CallToolResult, QueueJiraWriteOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		if in.Action == "update_story_points" {
			if in.RefreshStoryPointsField || !hasStoryPointsFieldMapping(in.Payload) {
				mapping, err := jirahooks.NewLifecycle(store, a.AdapterRegistry).ResolveStoryPointsField(ctx, in.ExecutionID, key+":story-points-field", in.RefreshStoryPointsField)
				if err != nil {
					return nil, QueueJiraWriteOutput{}, fmt.Errorf("mcpserver: discover Jira story-points field: %w", err)
				}
				payload := make(map[string]any, len(in.Payload)+1)
				for name, value := range in.Payload {
					payload[name] = value
				}
				payload["field_metadata"] = map[string]any{"id": mapping.FieldID, "name": mapping.FieldName}
				in.Payload = payload
			}
		}
		intent, err := store.CreateJiraWriteIntent(ctx, key, in.ExecutionID, in.SessionID, in.JiraIssueKey, in.Action, in.Payload)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, fmt.Errorf("mcpserver: queue Jira write: %w", err)
		}
		execution, err := store.GetExecutionByCase(ctx, intent.CaseID)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, err
		}
		view, err := store.BuildDeliveryView(ctx, execution.OrchestrationID)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, err
		}
		return nil, QueueJiraWriteOutput{Intent: *intent, View: *view}, nil
	}
}

func hasStoryPointsFieldMapping(payload map[string]any) bool {
	for _, key := range []string{"story_points_field_id", "storyPointsFieldId"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	metadata, ok := payload["field_metadata"].(map[string]any)
	if !ok {
		return false
	}
	value, ok := metadata["id"].(string)
	return ok && strings.TrimSpace(value) != ""
}

type MapDeliveryWorkItemInput struct {
	ExecutionID         string `json:"execution_id"`
	SessionID           string `json:"session_id,omitempty"`
	ParentTaskID        string `json:"parent_task_id"`
	RequirementSourceID string `json:"requirement_source_id"`
	JiraIssueKey        string `json:"jira_issue_key"`
	IdempotencyKey      string `json:"idempotency_key,omitempty"`
}
type MapDeliveryWorkItemOutput struct {
	Mapping delivery.JiraWorkItemMapping `json:"mapping"`
	View    delivery.DeliveryView        `json:"view"`
}

func mapDeliveryWorkItemHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, MapDeliveryWorkItemInput) (*mcp.CallToolResult, MapDeliveryWorkItemOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in MapDeliveryWorkItemInput) (*mcp.CallToolResult, MapDeliveryWorkItemOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, MapDeliveryWorkItemOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		mapping, err := store.MapWorkItemToJiraTask(ctx, key, in.ExecutionID, in.SessionID, in.ParentTaskID, in.RequirementSourceID, in.JiraIssueKey)
		if err != nil {
			return nil, MapDeliveryWorkItemOutput{}, fmt.Errorf("mcpserver: map delivery work item: %w", err)
		}
		view, err := store.BuildDeliveryView(ctx, mapping.OrchestrationID)
		if err != nil {
			return nil, MapDeliveryWorkItemOutput{}, err
		}
		return nil, MapDeliveryWorkItemOutput{Mapping: *mapping, View: *view}, nil
	}
}

type HydrateJiraDeliveryInput struct {
	ExecutionID    string `json:"execution_id"`
	SessionID      string `json:"session_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}
type HydrateJiraDeliveryOutput struct {
	Sources []jirahooks.HydratedJiraSource `json:"sources"`
	View    delivery.DeliveryView          `json:"view"`
}

func hydrateJiraDeliveryHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, HydrateJiraDeliveryInput) (*mcp.CallToolResult, HydrateJiraDeliveryOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in HydrateJiraDeliveryInput) (*mcp.CallToolResult, HydrateJiraDeliveryOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, HydrateJiraDeliveryOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		sources, err := jirahooks.NewLifecycle(store, a.AdapterRegistry).Hydrate(ctx, in.ExecutionID, in.SessionID, key)
		if err != nil {
			return nil, HydrateJiraDeliveryOutput{}, fmt.Errorf("mcpserver: hydrate Jira delivery: %w", err)
		}
		execution, err := store.GetExecution(ctx, in.ExecutionID)
		if err != nil {
			return nil, HydrateJiraDeliveryOutput{}, err
		}
		view, err := store.BuildDeliveryView(ctx, execution.OrchestrationID)
		if err != nil {
			return nil, HydrateJiraDeliveryOutput{}, err
		}
		return nil, HydrateJiraDeliveryOutput{Sources: sources, View: *view}, nil
	}
}

type ExecuteJiraWritesInput struct {
	IntentID       string `json:"intent_id,omitempty"`
	ExecutionID    string `json:"execution_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}
type ExecuteJiraWritesOutput struct {
	Intents []delivery.JiraWriteIntent `json:"intents"`
	View    delivery.DeliveryView      `json:"view"`
}

type CancelJiraWriteIntentInput struct {
	IntentID       string `json:"intent_id"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

func cancelJiraWriteIntentHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CancelJiraWriteIntentInput) (*mcp.CallToolResult, QueueJiraWriteOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CancelJiraWriteIntentInput) (*mcp.CallToolResult, QueueJiraWriteOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		intent, err := store.CancelJiraWriteIntent(ctx, key, in.IntentID)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, fmt.Errorf("mcpserver: cancel Jira write intent: %w", err)
		}
		execution, err := store.GetExecution(ctx, intent.ExecutionID)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, err
		}
		view, err := store.BuildDeliveryView(ctx, execution.OrchestrationID)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, err
		}
		return nil, QueueJiraWriteOutput{Intent: *intent, View: *view}, nil
	}
}

func executeJiraWritesHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ExecuteJiraWritesInput) (*mcp.CallToolResult, ExecuteJiraWritesOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ExecuteJiraWritesInput) (*mcp.CallToolResult, ExecuteJiraWritesOutput, error) {
		if (in.IntentID == "") == (in.ExecutionID == "") {
			return nil, ExecuteJiraWritesOutput{}, fmt.Errorf("mcpserver: execute Jira writes requires exactly one of intent_id or execution_id")
		}
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, ExecuteJiraWritesOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		lifecycle := jirahooks.NewLifecycle(store, a.AdapterRegistry)
		var intents []delivery.JiraWriteIntent
		var execution *delivery.DeliveryExecution
		if in.IntentID != "" {
			intent, err := lifecycle.Execute(ctx, in.IntentID, key)
			if err != nil {
				return nil, ExecuteJiraWritesOutput{}, fmt.Errorf("mcpserver: execute Jira write intent: %w", err)
			}
			intents = []delivery.JiraWriteIntent{*intent}
			execution, err = store.GetExecution(ctx, intent.ExecutionID)
			if err != nil {
				return nil, ExecuteJiraWritesOutput{}, err
			}
		} else {
			intents, err = lifecycle.ExecutePending(ctx, in.ExecutionID, key)
			if err != nil {
				return nil, ExecuteJiraWritesOutput{}, fmt.Errorf("mcpserver: execute pending Jira writes: %w", err)
			}
			execution, err = store.GetExecution(ctx, in.ExecutionID)
			if err != nil {
				return nil, ExecuteJiraWritesOutput{}, err
			}
		}
		view, err := store.BuildDeliveryView(ctx, execution.OrchestrationID)
		if err != nil {
			return nil, ExecuteJiraWritesOutput{}, err
		}
		return nil, ExecuteJiraWritesOutput{Intents: intents, View: *view}, nil
	}
}
