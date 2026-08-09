// scheduler.go implements the worker-scheduling core: the runnable
// frontier (reusing Frontier's pure graph-reachability logic), one
// mutating lane per project within an orchestration, and the lease
// lifecycle (grant/heartbeat/complete/reject/expire) every lane moves
// through.
//
// A global concurrency cap across all open orchestrations at once is
// deliberately not enforced here - correctly limiting it requires
// scanning every open orchestration's event log, not just this one's,
// which is left as a documented follow-up rather than built as a
// full-table scan in this pass. What is enforced, per orchestration, is
// one mutating lane per project.
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

// SyncFrontier recomputes which lanes are runnable versus blocked from
// the orchestration's current task graph: a lane whose parent task has
// no unresolved blocking predecessor moves to runnable;
// one with unresolved predecessors moves to blocked, recording the
// exact blocker task ids. Only lanes still in waiting or blocked move -
// a lane already leased, running, in review, failed, or accepted is
// never touched by a frontier recompute. Emits nothing when no lane's
// state actually needs to change, so calling this repeatedly (e.g. from
// a poll loop, or after every lease completion) is cheap and idempotent
// in effect even though idempotencyKey is fresh each call.
func (s *Store) SyncFrontier(ctx context.Context, idempotencyKey, orchestrationID string) ([]*protocol.DeliveryLane, error) {
	err := s.db.Write(ctx, idempotencyKey, "sync frontier "+orchestrationID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		tasks, err := allParentTasks(orchestrationID, events)
		if err != nil {
			return err
		}
		edges, err := allDependencyEdges(orchestrationID, events)
		if err != nil {
			return err
		}
		lanes, err := allLanes(orchestrationID, events)
		if err != nil {
			return err
		}

		laneByTask := map[string]*protocol.DeliveryLane{}
		for _, l := range lanes {
			if l.ParentTaskId != nil {
				laneByTask[*l.ParentTaskId] = l
			}
		}
		resolved := map[string]bool{}
		for taskID, l := range laneByTask {
			if l.Status == protocol.DeliveryLaneStatusAccepted {
				resolved[taskID] = true
			}
		}

		var activeEdges []*protocol.DependencyEdge
		for _, e := range edges {
			if e.Status == protocol.DependencyEdgeStatusActive {
				activeEdges = append(activeEdges, e)
			}
		}
		var taskList []*protocol.ParentTask
		for _, t := range tasks {
			taskList = append(taskList, t)
		}
		frontier, blocked := Frontier(taskList, activeEdges, resolved)

		frontierSet := map[string]bool{}
		for _, id := range frontier {
			frontierSet[id] = true
		}

		sequence := len(events)
		for taskID, lane := range laneByTask {
			if lane.Status != protocol.DeliveryLaneStatusWaiting && lane.Status != protocol.DeliveryLaneStatusBlocked {
				continue
			}
			if frontierSet[taskID] && lane.Status != protocol.DeliveryLaneStatusRunnable {
				if err := insertEvent(ctx, tx, eventRow{
					ID: newID(), OrchestrationID: orchestrationID, EntityID: &lane.Id, IdempotencyKey: idempotencyKey,
					Type: string(protocol.DeliveryEventTypeLaneUnblocked), Payload: "{}",
					Sequence: sequence, OccurredAt: time.Now().UTC(),
				}); err != nil {
					return err
				}
				sequence++
				continue
			}
			if blockers, ok := blocked[taskID]; ok && !sameBlockers(lane.BlockedBy, blockers) {
				payload, err := json.Marshal(map[string]interface{}{"blocked_by": blockers})
				if err != nil {
					return err
				}
				if err := insertEvent(ctx, tx, eventRow{
					ID: newID(), OrchestrationID: orchestrationID, EntityID: &lane.Id, IdempotencyKey: idempotencyKey,
					Type: string(protocol.DeliveryEventTypeLaneBlocked), Payload: string(payload),
					Sequence: sequence, OccurredAt: time.Now().UTC(),
				}); err != nil {
					return err
				}
				sequence++
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}

	events, err := loadEvents(ctx, s.db.Reader(), orchestrationID)
	if err != nil {
		return nil, err
	}
	laneMap, err := allLanes(orchestrationID, events)
	if err != nil {
		return nil, err
	}
	out := make([]*protocol.DeliveryLane, 0, len(laneMap))
	for _, l := range laneMap {
		out = append(out, l)
	}
	return out, nil
}

func sameBlockers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, id := range a {
		seen[id] = true
	}
	for _, id := range b {
		if !seen[id] {
			return false
		}
	}
	return true
}

// GrantLease claims lane for workerID, provided it is currently runnable
// and the project does not already have another mutating lane leased or
// running. expectedRevision is checked against the lane's current
// derived revision for optimistic concurrency, same as UpdateLaneStatus.
// The lease expires at leaseDuration from now unless renewed via
// Heartbeat.
func (s *Store) GrantLease(ctx context.Context, idempotencyKey, orchestrationID, laneID string, expectedRevision int, workerID string, leaseDuration time.Duration) (*protocol.DeliveryLane, error) {
	err := s.db.Write(ctx, idempotencyKey, "grant lease "+laneID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		lane, err := reduceLane(orchestrationID, laneID, events)
		if err != nil {
			return err
		}
		if lane.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if lane.Status != protocol.DeliveryLaneStatusRunnable {
			return ErrLaneNotRunnable
		}

		lanes, err := allLanes(orchestrationID, events)
		if err != nil {
			return err
		}
		for _, other := range lanes {
			if other.Id == laneID || other.ProjectId != lane.ProjectId {
				continue
			}
			if other.Status == protocol.DeliveryLaneStatusLeased || other.Status == protocol.DeliveryLaneStatusRunning {
				return ErrProjectAtConcurrencyLimit
			}
		}

		attempt := 1
		if lane.Attempt != nil {
			attempt = *lane.Attempt + 1
		}
		payload, err := json.Marshal(map[string]interface{}{
			"worker_id": workerID, "lease_token": newID(),
			"expires_at": time.Now().UTC().Add(leaseDuration).Format(timeLayout),
			"attempt":    attempt,
		})
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeLeaseGranted), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetLane(ctx, orchestrationID, laneID)
}

// Heartbeat renews an active lease, proving the worker is still alive
// and moving the lane to running on its first heartbeat after grant.
// leaseToken must match the lane's current lease, so a superseded or
// expired lease's holder cannot resurrect it.
func (s *Store) Heartbeat(ctx context.Context, idempotencyKey, orchestrationID, laneID, leaseToken string, expectedRevision int, leaseDuration time.Duration) (*protocol.DeliveryLane, error) {
	err := s.db.Write(ctx, idempotencyKey, "heartbeat "+laneID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		lane, err := reduceLane(orchestrationID, laneID, events)
		if err != nil {
			return err
		}
		if lane.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if lane.Status != protocol.DeliveryLaneStatusLeased && lane.Status != protocol.DeliveryLaneStatusRunning {
			return ErrLaneNotRunnable
		}
		if lane.LeaseToken == nil || *lane.LeaseToken != leaseToken {
			return ErrLeaseTokenMismatch
		}
		payload, err := json.Marshal(map[string]interface{}{
			"expires_at": time.Now().UTC().Add(leaseDuration).Format(timeLayout),
		})
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeLeaseHeartbeat), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetLane(ctx, orchestrationID, laneID)
}

// CompleteLease reports the leaseholder's work done, moving the lane to
// review; a later reviewer decides whether it moves to accepted or
// failed from there.
func (s *Store) CompleteLease(ctx context.Context, idempotencyKey, orchestrationID, laneID, leaseToken string, expectedRevision int) (*protocol.DeliveryLane, error) {
	return s.transitionLeasedLane(ctx, idempotencyKey, orchestrationID, laneID, leaseToken, expectedRevision, protocol.DeliveryEventTypeLeaseCompleted)
}

// RejectLease reports the leaseholder declining the work (e.g. a
// precondition it discovered no longer holds); the lane returns to
// runnable so another worker (or a retry) can pick it up.
func (s *Store) RejectLease(ctx context.Context, idempotencyKey, orchestrationID, laneID, leaseToken string, expectedRevision int) (*protocol.DeliveryLane, error) {
	return s.transitionLeasedLane(ctx, idempotencyKey, orchestrationID, laneID, leaseToken, expectedRevision, protocol.DeliveryEventTypeLeaseRejected)
}

func (s *Store) transitionLeasedLane(ctx context.Context, idempotencyKey, orchestrationID, laneID, leaseToken string, expectedRevision int, eventType protocol.DeliveryEventType) (*protocol.DeliveryLane, error) {
	err := s.db.Write(ctx, idempotencyKey, string(eventType)+" "+laneID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		lane, err := reduceLane(orchestrationID, laneID, events)
		if err != nil {
			return err
		}
		if lane.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if lane.Status != protocol.DeliveryLaneStatusLeased && lane.Status != protocol.DeliveryLaneStatusRunning {
			return ErrLaneNotRunnable
		}
		if lane.LeaseToken == nil || *lane.LeaseToken != leaseToken {
			return ErrLeaseTokenMismatch
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(eventType), Payload: "{}",
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetLane(ctx, orchestrationID, laneID)
}

// ExpireLeases sweeps every leased/running lane in orchestrationID whose
// lease_expires_at has passed and returns it to runnable, so a crashed
// worker's lease does not block its lane forever. A lane past review is
// never touched here, so an already-accepted result is never
// duplicated. Returns the ids of every lane reclaimed.
func (s *Store) ExpireLeases(ctx context.Context, idempotencyKey, orchestrationID string) ([]string, error) {
	var expired []string
	err := s.db.Write(ctx, idempotencyKey, "expire leases "+orchestrationID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		lanes, err := allLanes(orchestrationID, events)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		sequence := len(events)
		for _, lane := range lanes {
			if lane.Status != protocol.DeliveryLaneStatusLeased && lane.Status != protocol.DeliveryLaneStatusRunning {
				continue
			}
			if lane.LeaseExpiresAt == nil || lane.LeaseExpiresAt.After(now) {
				continue
			}
			if err := insertEvent(ctx, tx, eventRow{
				ID: newID(), OrchestrationID: orchestrationID, EntityID: &lane.Id, IdempotencyKey: idempotencyKey,
				Type: string(protocol.DeliveryEventTypeLeaseTimedOut), Payload: "{}",
				Sequence: sequence, OccurredAt: now,
			}); err != nil {
				return err
			}
			sequence++
			expired = append(expired, lane.Id)
		}
		return nil
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, fmt.Errorf("delivery: expire leases: %w", err)
	}
	return expired, nil
}
