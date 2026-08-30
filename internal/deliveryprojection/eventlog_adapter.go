package deliveryprojection

import (
	"context"
	"fmt"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// orchestrationState is one orchestration's reduced record and derived
// title, exactly what ListSummaries needs per delivery.
type orchestrationState struct {
	orchestration *protocol.DeliveryOrchestration
	title         string
}

// loadOrchestrationStates adapts internal/delivery's exported, batch-safe
// event-sourcing surface (LoadAllEvents, ReduceOrchestration,
// AllRequirementSources, OrchestrationTitle, SortedRequirementSources)
// into the shape ListSummaries iterates over, so the projector's own
// batch-query logic never has to touch delivery's event-reduction rules
// directly. ids reports every known orchestration id in the order its
// first event was recorded.
func loadOrchestrationStates(ctx context.Context, read reader) (states map[string]orchestrationState, ids []string, err error) {
	grouped, ids, err := delivery.LoadAllEvents(ctx, read)
	if err != nil {
		return nil, nil, fmt.Errorf("deliveryprojection: list summaries: %w", err)
	}
	states = make(map[string]orchestrationState, len(ids))
	for _, id := range ids {
		orch, err := delivery.ReduceOrchestration(id, grouped[id])
		if err != nil {
			return nil, nil, fmt.Errorf("deliveryprojection: reduce orchestration %s: %w", id, err)
		}
		sourceMap, err := delivery.AllRequirementSources(id, grouped[id])
		if err != nil {
			return nil, nil, fmt.Errorf("deliveryprojection: requirement sources %s: %w", id, err)
		}
		states[id] = orchestrationState{
			orchestration: orch,
			title:         delivery.OrchestrationTitle(orch, delivery.SortedRequirementSources(sourceMap)),
		}
	}
	return states, ids, nil
}
