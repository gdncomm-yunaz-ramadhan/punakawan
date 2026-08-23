package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// CaptureRequirement records a normalized requirement source into an
// open orchestration. Re-capturing an already-seen canonical_key with
// identical content is a harmless no-op; re-capturing with changed
// content records a new revision of the same source (its id is
// preserved, so anything already grouped into a ParentTask stays
// grouped). canonical_key is always an exact provider identifier (see
// CanonicalKey), never a fuzzy text match, so a pinned source can never
// be silently replaced by a similarly-worded one.
func (s *Store) CaptureRequirement(ctx context.Context, idempotencyKey, orchestrationID string, in SourceInput) (*protocol.RequirementSource, error) {
	canonicalKey, err := CanonicalKey(in)
	if err != nil {
		return nil, err
	}
	hash := contentHash(in)

	var resultID string
	err = s.db.Write(ctx, idempotencyKey, "capture requirement "+canonicalKey, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		orch, err := reduceOrchestration(orchestrationID, events)
		if err != nil {
			return err
		}
		if isTerminal(orch.Status) {
			return ErrInvalidState
		}

		existing, err := findByCanonicalKey(orchestrationID, canonicalKey, events)
		if err != nil {
			return err
		}
		if existing != nil && existing.ContentHash == hash {
			resultID = existing.Id
			return nil // identical re-capture: nothing to record
		}

		var parentSourceID string
		if in.ParentKey != "" {
			parentCanonicalKey, err := CanonicalKey(SourceInput{Provider: in.Provider, ExternalID: in.ParentKey})
			if err == nil {
				if parent, err := findByCanonicalKey(orchestrationID, parentCanonicalKey, events); err == nil && parent != nil {
					parentSourceID = parent.Id
				}
			}
		}

		id := ""
		if existing != nil {
			id = existing.Id
		} else {
			id = newID()
		}
		resultID = id

		payload := map[string]interface{}{
			"provider": in.Provider, "external_id": in.ExternalID, "canonical_key": canonicalKey,
			"content_hash": hash, "title": in.Title, "summary": in.Summary,
		}
		if parentSourceID != "" {
			payload["parent_source_id"] = parentSourceID
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &id, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeRequirementCaptured), Payload: string(encoded),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetRequirementSource(ctx, orchestrationID, resultID)
}

// GetRequirementSource fails closed (ErrNotFound) when sourceID does
// not exist within orchestrationID's own event log.
func (s *Store) GetRequirementSource(ctx context.Context, orchestrationID, sourceID string) (*protocol.RequirementSource, error) {
	events, err := loadEvents(ctx, s.db.Reader(), orchestrationID)
	if err != nil {
		return nil, err
	}
	return reduceRequirementSource(orchestrationID, sourceID, events)
}

// ListRequirementSources returns every requirement source captured into
// orchestrationID, ordered by capture time (ties broken by id) so the
// result is stable across calls. GetRequirementSource requires already
// knowing a source id, which a caller that only has the original
// reference strings never does; this is the enumeration that closes that
// gap, mirroring ListLanes/ListGraph's shape for lanes and tasks.
func (s *Store) ListRequirementSources(ctx context.Context, orchestrationID string) ([]*protocol.RequirementSource, error) {
	events, err := loadEvents(ctx, s.db.Reader(), orchestrationID)
	if err != nil {
		return nil, err
	}
	sourceMap, err := allRequirementSources(orchestrationID, events)
	if err != nil {
		return nil, err
	}
	return sortedRequirementSources(sourceMap), nil
}

// sortedRequirementSources flattens allRequirementSources' by-id map
// into capture order, ties broken by id so the result is stable across
// calls. "Oldest capture first" is also what makes "the first
// requirement" a well-defined thing for anything reading only the head
// of the list.
func sortedRequirementSources(sourceMap map[string]*protocol.RequirementSource) []*protocol.RequirementSource {
	sources := make([]*protocol.RequirementSource, 0, len(sourceMap))
	for _, src := range sourceMap {
		sources = append(sources, src)
	}
	sort.Slice(sources, func(i, j int) bool {
		if !sources[i].CapturedAt.Equal(sources[j].CapturedAt) {
			return sources[i].CapturedAt.Before(sources[j].CapturedAt)
		}
		return sources[i].Id < sources[j].Id
	})
	return sources
}

func isTerminal(status protocol.DeliveryOrchestrationStatus) bool {
	return status == protocol.DeliveryOrchestrationStatusCancelled || status == protocol.DeliveryOrchestrationStatusCompleted
}
