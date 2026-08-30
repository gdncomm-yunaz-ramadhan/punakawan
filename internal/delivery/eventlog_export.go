package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// LoadAllEvents loads every recorded delivery event in one query against
// q, grouping the rows by orchestration id. It is the shared
// implementation behind AllOrchestrationStates and any other batch
// caller (e.g. internal/deliveryprojection's list projector) that must
// replay every orchestration's event log without querying once per id -
// q is accepted as an interface rather than *Store so such a caller can
// route the one query through its own connection/counting wrapper. ids
// reports every known orchestration id in the order its first event was
// recorded.
func LoadAllEvents(ctx context.Context, q querier) (grouped map[string][]protocol.DeliveryEvent, ids []string, err error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, orchestration_id, entity_id, idempotency_key, type, payload, sequence, occurred_at FROM delivery_events ORDER BY orchestration_id, sequence`,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("delivery: query all events: %w", err)
	}
	defer rows.Close()

	grouped = map[string][]protocol.DeliveryEvent{}
	for rows.Next() {
		var (
			ev         protocol.DeliveryEvent
			entityID   sql.NullString
			payload    string
			occurredAt string
		)
		if err := rows.Scan(&ev.Id, &ev.OrchestrationId, &entityID, &ev.IdempotencyKey, &ev.Type, &payload, &ev.Sequence, &occurredAt); err != nil {
			return nil, nil, fmt.Errorf("delivery: scan event: %w", err)
		}
		if entityID.Valid {
			ev.EntityId = &entityID.String
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			return nil, nil, fmt.Errorf("delivery: decode payload for event %s: %w", ev.Id, err)
		}
		ev.Payload = decoded
		occurred, err := time.Parse(timeLayout, occurredAt)
		if err != nil {
			return nil, nil, fmt.Errorf("delivery: parse occurred_at for event %s: %w", ev.Id, err)
		}
		ev.OccurredAt = occurred
		if _, seen := grouped[ev.OrchestrationId]; !seen {
			ids = append(ids, ev.OrchestrationId)
		}
		grouped[ev.OrchestrationId] = append(grouped[ev.OrchestrationId], ev)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("delivery: read all events: %w", err)
	}
	return grouped, ids, nil
}

// OrchestrationState is one orchestration's reduced record plus its
// derived display title, exactly the two things AllOrchestrationStates
// computes per group.
type OrchestrationState struct {
	Orchestration *protocol.DeliveryOrchestration
	Title         string
}

// AllOrchestrationStates loads every recorded delivery event in a single
// query (LoadAllEvents), then reduces each group into its current
// orchestration record and derived title - the batch-safe alternative to
// calling GetOrchestration (or BuildDeliveryView) once per id, which is
// what ListOrchestrations already does and a list view over every
// delivery at once must not repeat. ids reports every known orchestration
// id in the order its first event was recorded.
func (s *Store) AllOrchestrationStates(ctx context.Context) (states map[string]OrchestrationState, ids []string, err error) {
	grouped, ids, err := LoadAllEvents(ctx, s.db.Reader())
	if err != nil {
		return nil, nil, err
	}
	states = make(map[string]OrchestrationState, len(ids))
	for _, id := range ids {
		events := grouped[id]
		orch, err := reduceOrchestration(id, events)
		if err != nil {
			return nil, nil, err
		}
		sourceMap, err := allRequirementSources(id, events)
		if err != nil {
			return nil, nil, err
		}
		states[id] = OrchestrationState{
			Orchestration: orch,
			Title:         orchestrationTitle(orch, sortedRequirementSources(sourceMap)),
		}
	}
	return states, ids, nil
}

// ReduceOrchestration, AllRequirementSources, OrchestrationTitle, and
// SortedRequirementSources expose their unexported, side-effect-free
// namesakes to internal/deliveryprojection, which needs to derive many
// orchestrations' state from one batch-loaded event log (LoadAllEvents)
// rather than duplicating this package's own event-reduction rules.
func ReduceOrchestration(id string, events []protocol.DeliveryEvent) (*protocol.DeliveryOrchestration, error) {
	return reduceOrchestration(id, events)
}

func AllRequirementSources(orchestrationID string, events []protocol.DeliveryEvent) (map[string]*protocol.RequirementSource, error) {
	return allRequirementSources(orchestrationID, events)
}

func OrchestrationTitle(orch *protocol.DeliveryOrchestration, sources []*protocol.RequirementSource) string {
	return orchestrationTitle(orch, sources)
}

func SortedRequirementSources(sourceMap map[string]*protocol.RequirementSource) []*protocol.RequirementSource {
	return sortedRequirementSources(sourceMap)
}
