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

// blockingEdgeTypes are the only edge types that gate execution
// (punokawan-14yn.2 design); serializes-with and informational edges
// are recorded but never block the runnable frontier.
var blockingEdgeTypes = map[protocol.DependencyEdgeType]bool{
	protocol.DependencyEdgeTypeRequires:         true,
	protocol.DependencyEdgeTypeProducesInputFor: true,
}

// AddDependencyEdge records fromTaskID depends on toTaskID, rejecting
// unknown task ids and any edge that would create a cycle.
func (s *Store) AddDependencyEdge(ctx context.Context, idempotencyKey, id, orchestrationID, fromTaskID, toTaskID string, edgeType protocol.DependencyEdgeType, origin protocol.DependencyEdgeOrigin, confidence float64, evidence string) (*protocol.DependencyEdge, error) {
	if fromTaskID == toTaskID {
		return nil, ErrCycle
	}
	err := s.db.Write(ctx, idempotencyKey, "add edge "+id, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		tasks, err := allParentTasks(orchestrationID, events)
		if err != nil {
			return err
		}
		if _, ok := tasks[fromTaskID]; !ok {
			return ErrNotFound
		}
		if _, ok := tasks[toTaskID]; !ok {
			return ErrNotFound
		}
		if crossesProjectsUnsafely(tasks[fromTaskID], tasks[toTaskID], origin) {
			return ErrUnsafeCrossProjectEdge
		}
		edges, err := allDependencyEdges(orchestrationID, events)
		if err != nil {
			return err
		}
		if reachable(edges, toTaskID, fromTaskID) {
			return ErrCycle
		}

		payload, err := json.Marshal(map[string]interface{}{
			"from_task_id": fromTaskID, "to_task_id": toTaskID, "type": string(edgeType),
			"origin": string(origin), "confidence": confidence, "evidence": evidence,
		})
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &id, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeEdgeAdded), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetDependencyEdge(ctx, orchestrationID, id)
}

// GetDependencyEdge fails closed (ErrNotFound) when edgeID does not
// exist within orchestrationID's own event log.
func (s *Store) GetDependencyEdge(ctx context.Context, orchestrationID, edgeID string) (*protocol.DependencyEdge, error) {
	events, err := loadEvents(ctx, s.db.Reader(), orchestrationID)
	if err != nil {
		return nil, err
	}
	return reduceDependencyEdge(orchestrationID, edgeID, events)
}

// RemoveDependencyEdge marks edgeID removed. Once the edge's from-task
// has been routed (routing stands in for "execution about to start" at
// this layer; actual worker execution is punokawan-14yn.3's concern),
// removal requires non-empty removalEvidence; before routing,
// reorganization is free.
func (s *Store) RemoveDependencyEdge(ctx context.Context, idempotencyKey, orchestrationID, edgeID, removalEvidence string) (*protocol.DependencyEdge, error) {
	err := s.db.Write(ctx, idempotencyKey, "remove edge "+edgeID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		edge, err := reduceDependencyEdge(orchestrationID, edgeID, events)
		if err != nil {
			return err
		}
		if edge.Status == protocol.DependencyEdgeStatusRemoved {
			return ErrInvalidState
		}
		fromTask, err := reduceParentTask(orchestrationID, edge.FromTaskId, events)
		if err != nil {
			return err
		}
		if fromTask.Status == protocol.ParentTaskStatusRouted && removalEvidence == "" {
			return ErrEvidenceRequired
		}
		payload, err := json.Marshal(map[string]interface{}{"removal_evidence": removalEvidence})
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &edgeID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeEdgeRemoved), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetDependencyEdge(ctx, orchestrationID, edgeID)
}

// ListGraph returns every non-cancelled task and active edge for
// orchestrationID, the inputs Frontier/Blocked need.
func (s *Store) ListGraph(ctx context.Context, orchestrationID string) ([]*protocol.ParentTask, []*protocol.DependencyEdge, error) {
	events, err := loadEvents(ctx, s.db.Reader(), orchestrationID)
	if err != nil {
		return nil, nil, err
	}
	taskMap, err := allParentTasks(orchestrationID, events)
	if err != nil {
		return nil, nil, err
	}
	edgeMap, err := allDependencyEdges(orchestrationID, events)
	if err != nil {
		return nil, nil, err
	}
	tasks := make([]*protocol.ParentTask, 0, len(taskMap))
	for _, t := range taskMap {
		tasks = append(tasks, t)
	}
	edges := make([]*protocol.DependencyEdge, 0, len(edgeMap))
	for _, e := range edgeMap {
		if e.Status == protocol.DependencyEdgeStatusActive {
			edges = append(edges, e)
		}
	}
	return tasks, edges, nil
}

// crossesProjectsUnsafely reports whether an edge between two tasks
// already routed to different projects is being added without an
// explicit (explicit-source or user) origin - an inferred cross-project
// coupling is never allowed to slip in silently.
func crossesProjectsUnsafely(from, to *protocol.ParentTask, origin protocol.DependencyEdgeOrigin) bool {
	if from.ProjectId == nil || to.ProjectId == nil || *from.ProjectId == *to.ProjectId {
		return false
	}
	return origin != protocol.DependencyEdgeOriginExplicitSource && origin != protocol.DependencyEdgeOriginUser
}

// reachable reports whether to is reachable from "from" by following
// active edges' from_task_id -> to_task_id direction (i.e. following
// dependency arrows forward, "from depends on to"). Used before adding
// a new fromTaskID -> toTaskID edge: if toTaskID can already reach
// fromTaskID, adding the new edge would close a cycle.
func reachable(edges map[string]*protocol.DependencyEdge, from, to string) bool {
	adjacency := map[string][]string{}
	for _, e := range edges {
		if e.Status != protocol.DependencyEdgeStatusActive {
			continue
		}
		adjacency[e.FromTaskId] = append(adjacency[e.FromTaskId], e.ToTaskId)
	}
	visited := map[string]bool{from: true}
	queue := []string{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == to {
			return true
		}
		for _, next := range adjacency[cur] {
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	return false
}

// Frontier returns the ids of every non-cancelled, not-yet-resolved
// task with no unresolved blocking predecessor, and, for every blocked
// task, the ids of every predecessor still keeping it blocked
// (punokawan-14yn.2 acceptance criterion 5). resolved marks which task
// ids are already considered done; callers (punokawan-14yn.3's
// scheduler) own what "resolved" means at execution time.
func Frontier(tasks []*protocol.ParentTask, edges []*protocol.DependencyEdge, resolved map[string]bool) (frontier []string, blocked map[string][]string) {
	predecessors := map[string][]string{}
	for _, e := range edges {
		if blockingEdgeTypes[e.Type] {
			predecessors[e.FromTaskId] = append(predecessors[e.FromTaskId], e.ToTaskId)
		}
	}

	blocked = map[string][]string{}
	for _, t := range tasks {
		if t.Status == protocol.ParentTaskStatusCancelled || resolved[t.Id] {
			continue
		}
		var unresolved []string
		for _, pred := range predecessors[t.Id] {
			if !resolved[pred] {
				unresolved = append(unresolved, pred)
			}
		}
		if len(unresolved) == 0 {
			frontier = append(frontier, t.Id)
		} else {
			blocked[t.Id] = unresolved
		}
	}
	return frontier, blocked
}
