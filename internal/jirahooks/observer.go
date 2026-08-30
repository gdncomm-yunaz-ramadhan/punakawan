package jirahooks

import (
	"context"
	"encoding/json"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/outbox"
)

// WorklogSyncObserver implements providerwrite.SuccessObserver: once a
// jira.worklog intent succeeds (whether by a fresh write or by
// reconciliation confirming one already applied), it marks the
// corresponding delivery_worklogs ledger entry synced with Jira's returned
// worklog id. It is the daemon-side counterpart to what
// jirahooks.Lifecycle.RetryWorkLogSync already does inline for its own
// synchronous callers.
type WorklogSyncObserver struct {
	store *delivery.Store
}

// NewWorklogSyncObserver builds a WorklogSyncObserver over store.
func NewWorklogSyncObserver(store *delivery.Store) *WorklogSyncObserver {
	return &WorklogSyncObserver{store: store}
}

// Observe implements providerwrite.SuccessObserver.
func (o *WorklogSyncObserver) Observe(ctx context.Context, intent outbox.Intent, externalID string, effects []outbox.Effect) error {
	if intent.Operation != "atlassian.addWorklog" || externalID == "" {
		return nil
	}
	var payload struct {
		WorklogEntryID string `json:"worklog_entry_id"`
	}
	if err := json.Unmarshal([]byte(intent.PayloadJSON), &payload); err != nil {
		return err
	}
	if payload.WorklogEntryID == "" {
		return nil
	}
	return o.store.MarkWorkLogSynced(ctx, intent.OrchestrationID, payload.WorklogEntryID, externalID)
}
