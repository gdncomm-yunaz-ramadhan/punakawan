package jirahooks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/deliveryhooks"
	"github.com/ygrip/punakawan/internal/storage"
)

// alreadyFired reports whether (deliveryID, eventType, revision) already
// has a jira_hook_dispatch row, meaning an earlier Handle call already
// completed its Jira side effect for this exact delivery event - a
// retried or re-delivered dispatch of the same event is then treated as
// already done rather than repeated.
func (h *JiraHook) alreadyFired(ctx context.Context, deliveryID string, eventType deliveryhooks.EventType, revision int) (bool, error) {
	var exists int
	err := h.db.Reader().QueryRowContext(ctx,
		`SELECT 1 FROM jira_hook_dispatch WHERE delivery_id = ? AND event_type = ? AND revision = ?`,
		deliveryID, string(eventType), revision,
	).Scan(&exists)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	default:
		return false, fmt.Errorf("jirahooks: query dispatch marker: %w", err)
	}
}

// markFired records that (deliveryID, eventType, revision) has now had its
// Jira side effect applied, using the storage kernel's own idempotency
// primitive (storage.DB.Write) so a concurrent duplicate insert is a safe
// no-op rather than a constraint-violation error. Called only after the
// Jira call(s) for this event have already succeeded (see Handle), so a
// failed call is never marked done and stays eligible for a future retry
// to actually apply it.
func (h *JiraHook) markFired(ctx context.Context, deliveryID string, eventType deliveryhooks.EventType, revision int, issueKey string) error {
	idempotencyKey := fmt.Sprintf("jira-hook-dispatch:%s:%s:%d", deliveryID, eventType, revision)
	err := h.db.Write(ctx, idempotencyKey, "jira hook dispatch "+string(eventType)+" "+deliveryID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO jira_hook_dispatch (delivery_id, event_type, revision, issue_key, fired_at) VALUES (?, ?, ?, ?, ?)`,
			deliveryID, string(eventType), revision, issueKey, time.Now().UTC().Format(time.RFC3339Nano),
		)
		return err
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return fmt.Errorf("jirahooks: record dispatch marker: %w", err)
	}
	return nil
}
