package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// eventRow is the persisted shape of one delivery_events row.
type eventRow struct {
	ID              string
	OrchestrationID string
	LaneID          *string
	IdempotencyKey  string
	Type            string
	Payload         string
	Sequence        int
	OccurredAt      time.Time
}

// querier is satisfied by both *sql.DB (reader pool) and *sql.Tx (the
// serialized writer, mid-transaction), so loadEvents/loadEventsTx share
// one implementation.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func insertEvent(ctx context.Context, tx *sql.Tx, e eventRow) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO delivery_events (id, orchestration_id, lane_id, idempotency_key, type, payload, sequence, occurred_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.OrchestrationID, e.LaneID, e.IdempotencyKey, e.Type, e.Payload, e.Sequence, e.OccurredAt.Format(timeLayout),
	)
	if err != nil {
		return fmt.Errorf("delivery: insert event %s: %w", e.Type, err)
	}
	return nil
}

func loadEvents(ctx context.Context, q querier, orchestrationID string) ([]protocol.DeliveryEvent, error) {
	return queryEvents(ctx, q, orchestrationID)
}

func loadEventsTx(ctx context.Context, tx *sql.Tx, orchestrationID string) ([]protocol.DeliveryEvent, error) {
	return queryEvents(ctx, tx, orchestrationID)
}

func queryEvents(ctx context.Context, q querier, orchestrationID string) ([]protocol.DeliveryEvent, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, orchestration_id, lane_id, idempotency_key, type, payload, sequence, occurred_at FROM delivery_events WHERE orchestration_id = ? ORDER BY sequence`,
		orchestrationID,
	)
	if err != nil {
		return nil, fmt.Errorf("delivery: query events for %s: %w", orchestrationID, err)
	}
	defer rows.Close()

	var out []protocol.DeliveryEvent
	for rows.Next() {
		var (
			ev         protocol.DeliveryEvent
			laneID     sql.NullString
			payload    string
			occurredAt string
		)
		if err := rows.Scan(&ev.Id, &ev.OrchestrationId, &laneID, &ev.IdempotencyKey, &ev.Type, &payload, &ev.Sequence, &occurredAt); err != nil {
			return nil, fmt.Errorf("delivery: scan event for %s: %w", orchestrationID, err)
		}
		if laneID.Valid {
			ev.LaneId = &laneID.String
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			return nil, fmt.Errorf("delivery: decode payload for event %s: %w", ev.Id, err)
		}
		ev.Payload = decoded
		t, err := time.Parse(timeLayout, occurredAt)
		if err != nil {
			return nil, fmt.Errorf("delivery: parse occurred_at for event %s: %w", ev.Id, err)
		}
		ev.OccurredAt = t
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("delivery: read events for %s: %w", orchestrationID, err)
	}
	return out, nil
}
