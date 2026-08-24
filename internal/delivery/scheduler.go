// scheduler.go implements the worker-scheduling core: the runnable
// frontier (reusing Frontier's pure graph-reachability logic), one
// mutating lane per project within an orchestration, a global cap on
// how many distinct projects may have mutating work in flight across
// every orchestration at once, and the lease lifecycle
// (grant/heartbeat/complete/reject/expire) every lane moves through.
//
// The global cap is computed with a full scan across every
// orchestration's own event log on each lease grant: an accurate count
// of "how many distinct projects currently have mutating work anywhere"
// genuinely needs every orchestration's own view of its lanes, not just
// the one being granted against, so there is no cheaper correct way to
// compute it without a separate materialized index this task does not
// introduce.
package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/deliveryhooks"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// SyncFrontier recomputes which lanes are runnable versus blocked from
// the orchestration's current task graph: a lane whose parent task has
// no unresolved blocking predecessor moves to runnable; one with
// unresolved predecessors moves to blocked, recording the exact
// blocker task ids. This can move a lane in either direction, including
// a runnable lane discovered to now have an unresolved predecessor
// (e.g. a dependency reported mid-execution) - so a lane not yet leased
// is never left runnable on a stale view of the graph. A lane already
// leased, running, in review, failed, or accepted is never touched: work
// already claimed or finished never gets retroactively paused or
// unwound by a later graph change. Emits nothing when no lane's state
// actually needs to change, so calling this repeatedly (e.g. from a
// poll loop, or after every lease completion) is cheap and idempotent
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
			if lane.Status != protocol.DeliveryLaneStatusWaiting &&
				lane.Status != protocol.DeliveryLaneStatusBlocked &&
				lane.Status != protocol.DeliveryLaneStatusRunnable {
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

// defaultMaxConcurrentProjects caps how many distinct projects may have
// mutating (leased or running) work in flight across every
// orchestration at once, unless a project already has work in flight
// and is simply continuing to use its own existing slot.
const defaultMaxConcurrentProjects = 4

// activeProjectIDs scans every orchestration's own event log and
// returns the set of project ids that currently have at least one
// leased or running lane, anywhere.
func activeProjectIDs(ctx context.Context, tx *sql.Tx) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT orchestration_id FROM delivery_events`)
	if err != nil {
		return nil, fmt.Errorf("delivery: list orchestrations: %w", err)
	}
	var orchestrationIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		orchestrationIDs = append(orchestrationIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	active := map[string]bool{}
	for _, id := range orchestrationIDs {
		events, err := loadEventsTx(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		lanes, err := allLanes(id, events)
		if err != nil {
			return nil, err
		}
		for _, l := range lanes {
			if l.Status == protocol.DeliveryLaneStatusLeased || l.Status == protocol.DeliveryLaneStatusRunning {
				active[l.ProjectId] = true
			}
		}
	}
	return active, nil
}

// GrantLease claims lane for workerID, provided it is currently
// runnable, the project does not already have another mutating lane
// leased or running within this orchestration, and granting it would
// not bring a new project into mutating work while the global cap is
// already full. expectedRevision is checked against the lane's current
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

		active, err := activeProjectIDs(ctx, tx)
		if err != nil {
			return err
		}
		if !active[lane.ProjectId] && len(active) >= defaultMaxConcurrentProjects {
			return ErrGlobalConcurrencyLimit
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
	fresh := err == nil
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	if fresh {
		// Only the fresh grant dispatches, never the duplicate-idempotency-
		// key retry - a retry lands on the same orchestration revision, and
		// re-dispatching "implementation started" for it would just make a
		// configured hook redo its own already-fired check for no reason.
		s.dispatchOrchestrationEvent(ctx, orchestrationID, deliveryhooks.EventImplementationStarted,
			"implementation started on lane "+laneID, nil)
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
// failed from there. A lane that never engages the role-stage flow at
// all (no Semar stage ever recorded) completes exactly as before - the
// stage gate only applies once a lane has actually opted in by
// recording its first stage, at which point it must reach Bagong's
// stage before it can be reported done (ErrRoleStagesIncomplete
// otherwise).
func (s *Store) CompleteLease(ctx context.Context, idempotencyKey, orchestrationID, laneID, leaseToken string, expectedRevision int) (*protocol.DeliveryLane, error) {
	lane, fresh, err := s.transitionLeasedLane(ctx, idempotencyKey, orchestrationID, laneID, leaseToken, expectedRevision, protocol.DeliveryEventTypeLeaseCompleted, func(lane *protocol.DeliveryLane, requiredStages map[string]bool) error {
		if lane.SemarRecordId == nil {
			return nil
		}
		stage, ok := lastRequiredStage(requiredStages)
		if !ok {
			return nil
		}
		id := recordIDForStage(lane, stage)
		if id == nil || *id == "" {
			return ErrRoleStagesIncomplete
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if fresh {
		// Only the fresh completion dispatches, never the duplicate-
		// idempotency-key retry, matching GrantLease's own reasoning above.
		s.dispatchOrchestrationEvent(ctx, orchestrationID, deliveryhooks.EventImplementationCompleted,
			"implementation completed on lane "+laneID, nil)
	}
	return lane, nil
}

// RejectLease reports the leaseholder declining the work (e.g. a
// precondition it discovered no longer holds); the lane returns to
// runnable so another worker (or a retry) can pick it up. Unlike
// CompleteLease, this never requires any role stage to have run - a
// worker may bail at any point.
func (s *Store) RejectLease(ctx context.Context, idempotencyKey, orchestrationID, laneID, leaseToken string, expectedRevision int) (*protocol.DeliveryLane, error) {
	lane, _, err := s.transitionLeasedLane(ctx, idempotencyKey, orchestrationID, laneID, leaseToken, expectedRevision, protocol.DeliveryEventTypeLeaseRejected, nil)
	return lane, err
}

// transitionLeasedLane returns fresh=true when this call actually appended
// the transition event (as opposed to replaying an already-committed
// idempotencyKey), so a caller that dispatches a hook event for this
// transition - currently only CompleteLease - can skip re-dispatching on a
// retry.
func (s *Store) transitionLeasedLane(ctx context.Context, idempotencyKey, orchestrationID, laneID, leaseToken string, expectedRevision int, eventType protocol.DeliveryEventType, precondition func(*protocol.DeliveryLane, map[string]bool) error) (lane *protocol.DeliveryLane, fresh bool, err error) {
	writeErr := s.db.Write(ctx, idempotencyKey, string(eventType)+" "+laneID, func(tx *sql.Tx) error {
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
		if precondition != nil {
			orch, err := reduceOrchestration(orchestrationID, events)
			if err != nil {
				return err
			}
			var workflowDefinitionID string
			if orch.WorkflowDefinitionId != nil {
				workflowDefinitionID = *orch.WorkflowDefinitionId
			}
			requiredStages, err := s.resolveRequiredStages(ctx, workflowDefinitionID)
			if err != nil {
				return err
			}
			if err := precondition(lane, requiredStages); err != nil {
				return err
			}
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(eventType), Payload: "{}",
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if writeErr != nil && !errors.Is(writeErr, storage.ErrDuplicateWrite) {
		return nil, false, writeErr
	}
	lane, err = s.GetLane(ctx, orchestrationID, laneID)
	if err != nil {
		return nil, false, err
	}
	return lane, writeErr == nil, nil
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
