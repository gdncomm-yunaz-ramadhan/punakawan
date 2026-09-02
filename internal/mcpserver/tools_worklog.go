package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/jirahooks"
)

// LogDeliveryWorkInput records an actual, task-bound interval of agent work.
// JiraIssueKey is intentionally required: the caller must identify the exact
// Jira subtask it worked on instead of allowing a delivery-wide guess.
type LogDeliveryWorkInput struct {
	OrchestrationID string    `json:"orchestration_id"`
	LaneID          string    `json:"lane_id"`
	SessionID       string    `json:"session_id,omitempty"`
	JiraIssueKey    string    `json:"jira_issue_key" jsonschema:"exact Jira issue or subtask key currently being worked"`
	StartedAt       time.Time `json:"started_at" jsonschema:"RFC3339 start time of measured work interval"`
	DurationSeconds int       `json:"duration_seconds" jsonschema:"positive measured work duration in seconds; never lease or wall-clock estimate"`
	Summary         string    `json:"summary"`
	WorkLogID       string    `json:"worklog_id" jsonschema:"caller-stable id for this measured interval; reuse on retry"`
	IdempotencyKey  string    `json:"idempotency_key"`
}

type LogDeliveryWorkOutput struct {
	WorkLog delivery.WorkLogEntry `json:"worklog"`
	View    delivery.DeliveryView `json:"view"`
}

// RetryWorkLogSyncInput identifies an already-recorded worklog to replay to
// Jira. It deliberately accepts no duration or summary: recovery must use the
// immutable values in the worklog ledger.
type RetryWorkLogSyncInput struct {
	OrchestrationID string `json:"orchestration_id"`
	WorkLogID       string `json:"worklog_id"`
}

type RetryWorkLogSyncOutput struct {
	WorkLog delivery.WorkLogEntry `json:"worklog"`
	View    delivery.DeliveryView `json:"view"`
}

func logDeliveryWorkHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, LogDeliveryWorkInput) (*mcp.CallToolResult, LogDeliveryWorkOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in LogDeliveryWorkInput) (*mcp.CallToolResult, LogDeliveryWorkOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, LogDeliveryWorkOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = in.WorkLogID
		}
		entry, err := store.RecordWorkLog(ctx, key, in.WorkLogID, in.OrchestrationID, in.LaneID, in.SessionID,
			in.JiraIssueKey, in.StartedAt, in.DurationSeconds, in.Summary)
		if err != nil {
			return nil, LogDeliveryWorkOutput{}, fmt.Errorf("mcpserver: record delivery work: %w", err)
		}
		// Recording work against an issue is the clearest possible touch
		// of it. TouchJiraWorkItem had no caller anywhere, so touch_count
		// was structurally always 0 and first/last_touched_at always
		// empty - a projected field that could never say anything. It
		// never fails the worklog it accompanies: the measured work is
		// already durably recorded by this point, and losing a counter is
		// a far smaller harm than reporting a recorded interval as an
		// error.
		if entry.ExecutionID != "" {
			if _, err := store.TouchJiraWorkItem(ctx, "touch:"+entry.ID, entry.ExecutionID, entry.SessionID, entry.JiraIssueKey, entry.CreatedAt); err != nil {
				slog.Warn("mcpserver: touch jira work item", "worklog_id", entry.ID, "jira_issue_key", entry.JiraIssueKey, "error", err)
			}
		}
		view, err := store.BuildDeliveryView(ctx, in.OrchestrationID)
		if err != nil {
			return nil, LogDeliveryWorkOutput{}, fmt.Errorf("mcpserver: build delivery view: %w", err)
		}
		return nil, LogDeliveryWorkOutput{WorkLog: *entry, View: *view}, nil
	}
}

func retryWorkLogSyncHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, RetryWorkLogSyncInput) (*mcp.CallToolResult, RetryWorkLogSyncOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in RetryWorkLogSyncInput) (*mcp.CallToolResult, RetryWorkLogSyncOutput, error) {
		if in.OrchestrationID == "" || in.WorkLogID == "" {
			return nil, RetryWorkLogSyncOutput{}, fmt.Errorf("mcpserver: retry worklog sync requires orchestration_id and worklog_id")
		}
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, RetryWorkLogSyncOutput{}, err
		}
		outboxStore, err := a.OpenOutbox()
		if err != nil {
			return nil, RetryWorkLogSyncOutput{}, err
		}
		worklog, err := jirahooks.NewLifecycle(store, a.AdapterRegistry, outboxStore).RetryWorkLogSync(ctx, in.OrchestrationID, in.WorkLogID)
		if err != nil {
			return nil, RetryWorkLogSyncOutput{}, fmt.Errorf("mcpserver: retry worklog sync: %w", err)
		}
		view, err := store.BuildDeliveryView(ctx, in.OrchestrationID)
		if err != nil {
			return nil, RetryWorkLogSyncOutput{}, fmt.Errorf("mcpserver: build delivery view: %w", err)
		}
		return nil, RetryWorkLogSyncOutput{WorkLog: *worklog, View: *view}, nil
	}
}
