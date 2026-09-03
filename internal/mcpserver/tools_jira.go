package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
	Clarity        string `json:"clarity" jsonschema:"clear | needs_clarification"`
	Rationale      string `json:"rationale" jsonschema:"why the source reads as it does; required. When the clarity is needs_clarification this is the question posted on the issue"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}
type AssessJiraDeliveryOutput struct {
	Assessment delivery.JiraAssessment `json:"assessment"`
	View       delivery.DeliveryView   `json:"view"`

	// SubtaskBreakdown and SubtaskBreakdownNote are a best-effort,
	// never-persisted read of the parent issue's subtasks with story
	// points/estimated hours where resolvable - see
	// jirahooks.SuggestSubtaskBreakdown. Absent (not fabricated) when
	// nothing could be resolved.
	SubtaskBreakdown     []jirahooks.SubtaskEstimate `json:"subtask_breakdown,omitempty"`
	SubtaskBreakdownNote string                      `json:"subtask_breakdown_note,omitempty"`
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
		out := AssessJiraDeliveryOutput{Assessment: *assessment, View: *view}
		out.SubtaskBreakdown, out.SubtaskBreakdownNote = suggestSubtaskBreakdown(ctx, a, store, execution.ID, key, view)
		return nil, out, nil
	}
}

// suggestSubtaskBreakdown is a best-effort, additive-only read of the
// delivery's subtask story points/hours - it must never fail or change
// assess_jira_delivery's existing Assessment/View return values, so every
// failure here degrades to an empty result rather than propagating.
func suggestSubtaskBreakdown(ctx context.Context, a *app.App, store *delivery.Store, executionID, idempotencyKey string, view *delivery.DeliveryView) ([]jirahooks.SubtaskEstimate, string) {
	outboxStore, err := a.OpenOutbox()
	if err != nil {
		return nil, ""
	}
	cfg, err := a.JiraWorkflow()
	if err != nil {
		cfg = nil
	}
	lifecycle := jirahooks.NewLifecycle(store, a.AdapterRegistry, outboxStore)
	breakdown, note := lifecycle.SuggestSubtaskBreakdown(ctx, executionID, idempotencyKey+":breakdown", cfg)
	if len(breakdown) == 0 || view.Lifecycle == nil {
		return breakdown, note
	}

	covered := map[string]bool{}
	for _, item := range view.Lifecycle.WorkItems {
		covered[strings.ToUpper(item.JiraIssueKey)] = true
	}
	var uncovered []string
	for _, estimate := range breakdown {
		key := strings.ToUpper(strings.TrimSpace(estimate.IssueKey))
		if key == "" || covered[key] {
			continue
		}
		uncovered = append(uncovered, estimate.IssueKey)
	}
	if len(uncovered) == 0 {
		return breakdown, note
	}
	sort.Strings(uncovered)
	uncoveredNote := fmt.Sprintf("%d subtask(s) not yet reflected as start_delivery tasks: %s", len(uncovered), strings.Join(uncovered, ", "))
	if note == "" {
		return breakdown, uncoveredNote
	}
	return breakdown, note + "; " + uncoveredNote
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
