package mcpserver

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ygrip/punakawan/internal/delivery"
)

// touchJiraIssue records that this tool call engaged an already-mapped Jira
// issue. It never fails its caller: by the time it runs, the call's real
// effect is already durably recorded, and losing a counter is a far smaller
// harm than reporting a completed write as an error. A call against an
// issue this delivery never mapped is not a fault at all - the touch simply
// has nothing to enrich.
func touchJiraIssue(ctx context.Context, store *delivery.Store, key, executionID, sessionID, issueKey string, at time.Time) {
	if _, err := store.TouchJiraWorkItem(ctx, key, executionID, sessionID, issueKey, at); err != nil && !errors.Is(err, delivery.ErrNotFound) {
		slog.Warn("mcpserver: touch jira work item", "jira_issue_key", issueKey, "error", err)
	}
}

// touchJiraIssueForTask is touchJiraIssue for a caller that knows the task
// it just finished rather than the issue that task was mapped to.
func touchJiraIssueForTask(ctx context.Context, store *delivery.Store, key, orchestrationID, parentTaskID, sessionID string, at time.Time) {
	if _, err := store.TouchJiraWorkItemForTask(ctx, key, orchestrationID, parentTaskID, sessionID, at); err != nil && !errors.Is(err, delivery.ErrNotFound) {
		slog.Warn("mcpserver: touch jira work item for task", "parent_task_id", parentTaskID, "error", err)
	}
}
