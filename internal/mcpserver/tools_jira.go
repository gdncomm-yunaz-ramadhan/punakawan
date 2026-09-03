package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/jirahooks"
)

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
		// Reading an issue closely enough to judge its clarity is a touch of
		// it, when the snapshot assessed names an issue this delivery mapped.
		if snapshotID != "" {
			if snapshot, err := store.GetJiraSnapshot(ctx, snapshotID); err == nil {
				touchJiraIssue(ctx, store, key+":touch", in.ExecutionID, in.SessionID, snapshot.JiraIssueKey, assessment.AssessedAt)
			}
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
		// Binding a task to an issue is an engagement with that issue, so
		// the mapping's own first_touched_at is the moment it was made
		// rather than whenever some later tool happened to reach it.
		touchJiraIssue(ctx, store, key+":touch", mapping.ExecutionID, in.SessionID, mapping.JiraIssueKey, mapping.CreatedAt)
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
		outboxStore, err := a.OpenOutbox()
		if err != nil {
			return nil, HydrateJiraDeliveryOutput{}, err
		}
		sources, err := jirahooks.NewLifecycle(store, a.AdapterRegistry, outboxStore).Hydrate(ctx, in.ExecutionID, in.SessionID, key)
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
