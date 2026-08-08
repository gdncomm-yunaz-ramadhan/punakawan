package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// CreateParentTask groups one or more already-captured requirement
// sources into a new graph node, unrouted until RouteParentTask assigns
// it a project.
func (s *Store) CreateParentTask(ctx context.Context, idempotencyKey, id, orchestrationID, title string, sourceIDs []string) (*protocol.ParentTask, error) {
	if len(sourceIDs) == 0 {
		return nil, errors.New("delivery: a parent task requires at least one source id")
	}
	err := s.db.Write(ctx, idempotencyKey, "create task "+id, func(tx *sql.Tx) error {
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
		sources, err := allRequirementSources(orchestrationID, events)
		if err != nil {
			return err
		}
		for _, sourceID := range sourceIDs {
			if _, ok := sources[sourceID]; !ok {
				return ErrNotFound
			}
		}
		payload, err := json.Marshal(map[string]interface{}{"title": title, "source_ids": sourceIDs})
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &id, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeTaskCreated), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetParentTask(ctx, orchestrationID, id)
}

// GetParentTask fails closed (ErrNotFound) when taskID does not exist
// within orchestrationID's own event log.
func (s *Store) GetParentTask(ctx context.Context, orchestrationID, taskID string) (*protocol.ParentTask, error) {
	events, err := loadEvents(ctx, s.db.Reader(), orchestrationID)
	if err != nil {
		return nil, err
	}
	return reduceParentTask(orchestrationID, taskID, events)
}

// RouteParentTask assigns taskID to projectID. Routing only ever comes
// from an exact match the caller already resolved (source binding,
// remote, project key, space, URL pattern, or a learned binding);
// ambiguous evidence is a caller-side decision (asking one batched
// location question) made before calling this, not something Store
// guesses at.
func (s *Store) RouteParentTask(ctx context.Context, idempotencyKey, orchestrationID, taskID, projectID string) (*protocol.ParentTask, error) {
	err := s.db.Write(ctx, idempotencyKey, "route task "+taskID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		task, err := reduceParentTask(orchestrationID, taskID, events)
		if err != nil {
			return err
		}
		if task.Status == protocol.ParentTaskStatusCancelled {
			return ErrInvalidState
		}
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM delivery_projects WHERE id = ?`, projectID).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if status != string(protocol.DeliveryProjectStatusActive) {
			return ErrProjectInactive
		}
		payload, err := json.Marshal(map[string]interface{}{"project_id": projectID})
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &taskID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeTaskRouted), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetParentTask(ctx, orchestrationID, taskID)
}
