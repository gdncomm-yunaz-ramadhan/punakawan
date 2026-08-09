// Package delivery is the canonical multi-project delivery control plane
// (punokawan-14yn.1): project registry, orchestrations, and delivery
// lanes, persisted through the SQLite storage kernel (internal/storage).
// Orchestration and lane state is never written directly — it is derived
// by replaying an append-only, idempotent event log (see reduce.go).
package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

const timeLayout = time.RFC3339Nano

// Store is the control plane's persistence boundary. It never falls
// back to an ambient working-directory project: every call is scoped by
// an explicit id.
type Store struct {
	db *storage.DB
}

// NewStore wraps an opened storage kernel database.
func NewStore(db *storage.DB) *Store { return &Store{db: db} }

// NewID mints a filesystem-safe ULID for a new project, orchestration,
// or lane. Callers generate ids up front so creation calls are
// idempotent on retry (the same id plus the same idempotency key is a
// no-op, not a duplicate).
func NewID() string { return newID() }

// RegisterProject adds a project to the registry. A duplicate slug
// fails; a duplicate idempotencyKey is harmless and returns the
// already-registered project.
func (s *Store) RegisterProject(ctx context.Context, idempotencyKey, id, slug, repositoryURL, defaultBranch string) (*protocol.DeliveryProject, error) {
	now := time.Now().UTC()
	err := s.db.Write(ctx, idempotencyKey, "register project "+slug, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO delivery_projects (id, slug, repository_url, default_branch, status, registered_at, revision) VALUES (?, ?, ?, ?, 'active', ?, 0)`,
			id, slug, repositoryURL, defaultBranch, now.Format(timeLayout),
		)
		return err
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetProject(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("delivery: register project %s: %w", slug, err)
	}
	return s.GetProject(ctx, id)
}

// GetProject fails closed (ErrNotFound) for an unknown project id.
func (s *Store) GetProject(ctx context.Context, id string) (*protocol.DeliveryProject, error) {
	row := s.db.Reader().QueryRowContext(ctx,
		`SELECT id, slug, repository_url, default_branch, status, registered_at, revision FROM delivery_projects WHERE id = ?`, id)
	var p protocol.DeliveryProject
	var defaultBranch, registeredAt string
	if err := row.Scan(&p.Id, &p.Slug, &p.RepositoryUrl, &defaultBranch, &p.Status, &registeredAt, &p.Revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("delivery: get project %s: %w", id, err)
	}
	if defaultBranch != "" {
		p.DefaultBranch = &defaultBranch
	}
	t, err := time.Parse(timeLayout, registeredAt)
	if err != nil {
		return nil, fmt.Errorf("delivery: parse registered_at for project %s: %w", id, err)
	}
	p.RegisteredAt = t
	return &p, nil
}

// CreateOrchestration appends the orchestration.created event that
// establishes id. A duplicate idempotencyKey is harmless and returns
// the already-created orchestration.
func (s *Store) CreateOrchestration(ctx context.Context, idempotencyKey, id string, inputs []protocol.DeliveryOrchestrationUnresolvedInputsElem) (*protocol.DeliveryOrchestration, error) {
	if inputs == nil {
		inputs = []protocol.DeliveryOrchestrationUnresolvedInputsElem{}
	}
	payload, err := json.Marshal(map[string]interface{}{"unresolved_inputs": inputs})
	if err != nil {
		return nil, fmt.Errorf("delivery: encode orchestration.created payload: %w", err)
	}
	now := time.Now().UTC()
	err = s.db.Write(ctx, idempotencyKey, "create orchestration "+id, func(tx *sql.Tx) error {
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: id, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeOrchestrationCreated), Payload: string(payload),
			Sequence: 0, OccurredAt: now,
		})
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetOrchestration(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("delivery: create orchestration %s: %w", id, err)
	}
	return s.GetOrchestration(ctx, id)
}

// GetOrchestration fails closed (ErrNotFound) for an unknown id.
func (s *Store) GetOrchestration(ctx context.Context, id string) (*protocol.DeliveryOrchestration, error) {
	events, err := loadEvents(ctx, s.db.Reader(), id)
	if err != nil {
		return nil, err
	}
	return reduceOrchestration(id, events)
}

// CancelOrchestration appends orchestration.cancelled after checking
// expectedRevision against the current derived revision and that the
// orchestration is not already terminal.
func (s *Store) CancelOrchestration(ctx context.Context, idempotencyKey, id string, expectedRevision int) (*protocol.DeliveryOrchestration, error) {
	err := s.db.Write(ctx, idempotencyKey, "cancel orchestration "+id, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, id)
		if err != nil {
			return err
		}
		current, err := reduceOrchestration(id, events)
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if isTerminal(current.Status) {
			return ErrInvalidState
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: id, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeOrchestrationCancelled), Payload: "{}",
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if errors.Is(err, storage.ErrDuplicateWrite) || err == nil {
		return s.GetOrchestration(ctx, id)
	}
	return nil, err
}

// RegisterInput appends input.registered, adding one more not-yet-routed
// requirement reference to the orchestration.
func (s *Store) RegisterInput(ctx context.Context, idempotencyKey, orchestrationID string, expectedRevision int, input protocol.DeliveryOrchestrationUnresolvedInputsElem) (*protocol.DeliveryOrchestration, error) {
	payload := map[string]interface{}{"reference": input.Reference}
	if input.Note != nil {
		payload["note"] = *input.Note
	}
	return s.appendOrchestrationEvent(ctx, idempotencyKey, orchestrationID, expectedRevision, protocol.DeliveryEventTypeInputRegistered, payload)
}

// ResolveInput appends input.resolved, removing a requirement reference
// once punokawan-14yn.2 has routed it to a project.
func (s *Store) ResolveInput(ctx context.Context, idempotencyKey, orchestrationID string, expectedRevision int, reference string) (*protocol.DeliveryOrchestration, error) {
	return s.appendOrchestrationEvent(ctx, idempotencyKey, orchestrationID, expectedRevision, protocol.DeliveryEventTypeInputResolved, map[string]interface{}{"reference": reference})
}

func (s *Store) appendOrchestrationEvent(ctx context.Context, idempotencyKey, orchestrationID string, expectedRevision int, eventType protocol.DeliveryEventType, payload map[string]interface{}) (*protocol.DeliveryOrchestration, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("delivery: encode %s payload: %w", eventType, err)
	}
	err = s.db.Write(ctx, idempotencyKey, string(eventType)+" "+orchestrationID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		current, err := reduceOrchestration(orchestrationID, events)
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if isTerminal(current.Status) {
			return ErrInvalidState
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, IdempotencyKey: idempotencyKey,
			Type: string(eventType), Payload: string(encoded),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if errors.Is(err, storage.ErrDuplicateWrite) || err == nil {
		return s.GetOrchestration(ctx, orchestrationID)
	}
	return nil, err
}

// CreateLane appends lane.created after verifying the orchestration is
// open and the project exists and is active. If parentTaskID is
// non-empty (a lane may legitimately be created before a task is
// assigned to it, per DeliveryLane's own parent_task_id field), the
// task must actually exist in this orchestration and, if it is
// already routed, must be routed to this same projectID
// (ErrScopeMismatch otherwise) - a lane's project scope must always
// agree with its own task's routing, never silently diverge from it.
// A lane's project scope is fixed at creation and checked on every
// later call against it.
func (s *Store) CreateLane(ctx context.Context, idempotencyKey, id, orchestrationID, projectID, parentTaskID string) (*protocol.DeliveryLane, error) {
	err := s.db.Write(ctx, idempotencyKey, "create lane "+id, func(tx *sql.Tx) error {
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

		if parentTaskID != "" {
			task, err := reduceParentTask(orchestrationID, parentTaskID, events)
			if err != nil {
				return err
			}
			if task.ProjectId != nil && *task.ProjectId != projectID {
				return ErrScopeMismatch
			}
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

		payload, err := json.Marshal(map[string]interface{}{"project_id": projectID, "parent_task_id": parentTaskID})
		if err != nil {
			return err
		}
		laneID := id
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeLaneCreated), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetLane(ctx, orchestrationID, id)
	}
	if err != nil {
		return nil, fmt.Errorf("delivery: create lane %s: %w", id, err)
	}
	return s.GetLane(ctx, orchestrationID, id)
}

// GetLane fails closed (ErrNotFound) when laneID does not exist within
// orchestrationID's own event log — a lane id from a different
// orchestration is never visible through this call.
func (s *Store) GetLane(ctx context.Context, orchestrationID, laneID string) (*protocol.DeliveryLane, error) {
	events, err := loadEvents(ctx, s.db.Reader(), orchestrationID)
	if err != nil {
		return nil, err
	}
	return reduceLane(orchestrationID, laneID, events)
}

// UpdateLaneStatus appends lane.status_changed after checking
// expectedRevision against the lane's current derived revision.
func (s *Store) UpdateLaneStatus(ctx context.Context, idempotencyKey, orchestrationID, laneID string, expectedRevision int, status protocol.DeliveryLaneStatus) (*protocol.DeliveryLane, error) {
	err := s.db.Write(ctx, idempotencyKey, "update lane "+laneID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		current, err := reduceLane(orchestrationID, laneID, events)
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		payload, err := json.Marshal(map[string]interface{}{"status": string(status)})
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeLaneStatusChanged), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetLane(ctx, orchestrationID, laneID)
	}
	if err != nil {
		return nil, err
	}
	return s.GetLane(ctx, orchestrationID, laneID)
}
