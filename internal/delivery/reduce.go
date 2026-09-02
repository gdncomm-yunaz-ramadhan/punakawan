package delivery

import (
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// reduceOrchestration derives an orchestration's current state by
// replaying its event log in sequence order. It is a pure function of
// events: the same ordered log always produces the same state, which is
// what makes replay deterministic.
func reduceOrchestration(id string, events []protocol.DeliveryEvent) (*protocol.DeliveryOrchestration, error) {
	if len(events) == 0 {
		return nil, ErrNotFound
	}
	if events[0].Type != protocol.DeliveryEventTypeOrchestrationCreated {
		return nil, fmt.Errorf("delivery: orchestration %s event log does not start with orchestration.created", id)
	}

	o := &protocol.DeliveryOrchestration{
		Id:               id,
		Status:           protocol.DeliveryOrchestrationStatusPending,
		UnresolvedInputs: []protocol.DeliveryOrchestrationUnresolvedInputsElem{},
		CreatedAt:        events[0].OccurredAt,
	}

	for i, ev := range events {
		if ev.EntityId != nil {
			continue // lane-scoped event, not part of orchestration state
		}
		o.UpdatedAt = ev.OccurredAt
		o.Revision++

		switch ev.Type {
		case protocol.DeliveryEventTypeOrchestrationCreated:
			if inputs, ok := ev.Payload["unresolved_inputs"]; ok {
				elems, err := decodeUnresolvedInputs(inputs)
				if err != nil {
					return nil, err
				}
				o.UnresolvedInputs = elems
			}
			if defID, ok := ev.Payload["workflow_definition_id"].(string); ok && defID != "" {
				o.WorkflowDefinitionId = &defID
			}
			// Title and description are optional here, so a log without
			// them simply leaves both fields nil - every orchestration
			// recorded before they existed replays exactly as it always
			// did. Deriving a readable label for a titleless run is a
			// read-model concern (deliveryview.go), not something this
			// reducer invents and hands back as if it had been persisted;
			// a description nobody wrote is never substituted at all.
			applyOptionalText(&o.Title, ev.Payload, "title")
			applyOptionalText(&o.Description, ev.Payload, "description")
			applyOptionalText(&o.PlanId, ev.Payload, "plan_id")
			applyOptionalInt(&o.PlanRevision, ev.Payload, "plan_revision")
		case protocol.DeliveryEventTypeOrchestrationDetailsUpdated:
			// Only the keys this event actually carries move; the rest of
			// the run's description is left exactly as an earlier event
			// left it, so editing a title never quietly erases prose
			// somebody else wrote.
			applyOptionalText(&o.Title, ev.Payload, "title")
			applyOptionalText(&o.Description, ev.Payload, "description")
			applyOptionalText(&o.PlanRecordId, ev.Payload, "plan_record_id")
			applyOptionalText(&o.PlanId, ev.Payload, "plan_id")
			applyOptionalInt(&o.PlanRevision, ev.Payload, "plan_revision")
			applyOptionalText(&o.SessionId, ev.Payload, "session_id")
		case protocol.DeliveryEventTypeProjectAttached:
			// Attachment order is the order projects were named, and a
			// project already attached is not appended twice - the field
			// is a set of the projects this run involves, not a log of how
			// often somebody said so.
			if projectID := stringField(ev.Payload, "project_id"); projectID != "" && indexOfString(o.ProjectIds, projectID) < 0 {
				o.ProjectIds = append(o.ProjectIds, projectID)
			}
		case protocol.DeliveryEventTypeProjectDetached:
			if i := indexOfString(o.ProjectIds, stringField(ev.Payload, "project_id")); i >= 0 {
				o.ProjectIds = append(o.ProjectIds[:i], o.ProjectIds[i+1:]...)
			}
		case protocol.DeliveryEventTypeInputRegistered:
			ref, _ := ev.Payload["reference"].(string)
			note, _ := ev.Payload["note"].(string)
			elem := protocol.DeliveryOrchestrationUnresolvedInputsElem{Reference: ref}
			if note != "" {
				elem.Note = &note
			}
			o.UnresolvedInputs = append(o.UnresolvedInputs, elem)
		case protocol.DeliveryEventTypeInputResolved:
			ref, _ := ev.Payload["reference"].(string)
			kept := o.UnresolvedInputs[:0]
			for _, e := range o.UnresolvedInputs {
				if e.Reference != ref {
					kept = append(kept, e)
				}
			}
			o.UnresolvedInputs = kept
		case protocol.DeliveryEventTypeOrchestrationCancelled:
			o.Status = protocol.DeliveryOrchestrationStatusCancelled
		case protocol.DeliveryEventTypeOrchestrationCompleted:
			o.Status = protocol.DeliveryOrchestrationStatusCompleted
		case protocol.DeliveryEventTypeOrchestrationCompletedWithGaps:
			// Purely a record of what completing this delivery skipped -
			// it always follows the orchestration.completed it annotates,
			// and changes nothing about the orchestration itself.
			continue
		default:
			return nil, fmt.Errorf("delivery: unknown orchestration event type %q", ev.Type)
		}

		// The first (orchestration.created) event leaves status pending;
		// any later orchestration-scoped event promotes it to active,
		// unless that event itself was a terminal transition.
		if i > 0 && o.Status == protocol.DeliveryOrchestrationStatusPending {
			o.Status = protocol.DeliveryOrchestrationStatusActive
		}
	}
	return o, nil
}

// reduceLane derives a lane's current state from the lane-scoped subset
// of its orchestration's event log.
func reduceLane(orchestrationID, laneID string, events []protocol.DeliveryEvent) (*protocol.DeliveryLane, error) {
	var laneEvents []protocol.DeliveryEvent
	for _, ev := range events {
		if ev.EntityId != nil && *ev.EntityId == laneID {
			laneEvents = append(laneEvents, ev)
		}
	}
	if len(laneEvents) == 0 {
		return nil, ErrNotFound
	}
	if laneEvents[0].Type != protocol.DeliveryEventTypeLaneCreated {
		return nil, fmt.Errorf("delivery: lane %s event log does not start with lane.created", laneID)
	}

	projectID, _ := laneEvents[0].Payload["project_id"].(string)
	if projectID == "" {
		return nil, fmt.Errorf("delivery: lane %s created without project_id", laneID)
	}

	l := &protocol.DeliveryLane{
		Id:              laneID,
		OrchestrationId: orchestrationID,
		ProjectId:       projectID,
		Status:          protocol.DeliveryLaneStatusWaiting,
		CreatedAt:       laneEvents[0].OccurredAt,
	}
	if parentTaskID, ok := laneEvents[0].Payload["parent_task_id"].(string); ok && parentTaskID != "" {
		l.ParentTaskId = &parentTaskID
	}
	// The session that opened the lane is fixed at creation, so it is read
	// from lane.created alongside project and parent task rather than
	// folded in the loop below. A lane created without one keeps it nil,
	// which is every lane recorded before lanes named a session.
	applyOptionalText(&l.SessionId, laneEvents[0].Payload, "session_id")

	for _, ev := range laneEvents {
		l.UpdatedAt = ev.OccurredAt
		l.Revision++
		switch ev.Type {
		case protocol.DeliveryEventTypeLaneCreated:
			// fields already applied above from laneEvents[0]
		case protocol.DeliveryEventTypeLaneStatusChanged:
			status, _ := ev.Payload["status"].(string)
			l.Status = protocol.DeliveryLaneStatus(status)
		case protocol.DeliveryEventTypeLaneBlocked:
			l.Status = protocol.DeliveryLaneStatusBlocked
			l.BlockedBy = stringSliceField(ev.Payload, "blocked_by")
		case protocol.DeliveryEventTypeLaneUnblocked:
			l.Status = protocol.DeliveryLaneStatusRunnable
			l.BlockedBy = nil
		case protocol.DeliveryEventTypeLeaseGranted:
			l.Status = protocol.DeliveryLaneStatusLeased
			workerID := stringField(ev.Payload, "worker_id")
			token := stringField(ev.Payload, "lease_token")
			l.LeaseWorkerId = &workerID
			l.LeaseToken = &token
			if expiresAt, ok := ev.Payload["expires_at"].(string); ok {
				if t, err := time.Parse(timeLayout, expiresAt); err == nil {
					l.LeaseExpiresAt = &t
				}
			}
			attempt := int(numberField(ev.Payload, "attempt"))
			l.Attempt = &attempt
		case protocol.DeliveryEventTypeLeaseHeartbeat:
			l.Status = protocol.DeliveryLaneStatusRunning
			if expiresAt, ok := ev.Payload["expires_at"].(string); ok {
				if t, err := time.Parse(timeLayout, expiresAt); err == nil {
					l.LeaseExpiresAt = &t
				}
			}
		case protocol.DeliveryEventTypeLeaseCompleted:
			l.Status = protocol.DeliveryLaneStatusReview
		case protocol.DeliveryEventTypeLeaseRejected, protocol.DeliveryEventTypeLeaseTimedOut:
			l.Status = protocol.DeliveryLaneStatusRunnable
			l.LeaseWorkerId, l.LeaseToken, l.LeaseExpiresAt = nil, nil, nil
		case protocol.DeliveryEventTypeLeaseCancelled:
			l.Status = protocol.DeliveryLaneStatusFailed
			l.LeaseWorkerId, l.LeaseToken, l.LeaseExpiresAt = nil, nil, nil
		case protocol.DeliveryEventTypeLaneWorktreeCreated:
			worktreePath := stringField(ev.Payload, "worktree_path")
			branch := stringField(ev.Payload, "branch")
			baseSha := stringField(ev.Payload, "base_sha")
			baseRemote := stringField(ev.Payload, "base_remote")
			l.WorktreePath = &worktreePath
			l.Branch = &branch
			l.BaseSha = &baseSha
			l.BaseRemote = &baseRemote
		case protocol.DeliveryEventTypeLaneWorktreeRemoved:
			// Branch/BaseSha/BaseRemote deliberately survive removal: a
			// lane's work commonly spans more than one create/remove
			// round, and checking the same branch back out preserves its
			// existing commits instead of starting a fresh, disconnected
			// branch each time.
			l.WorktreePath = nil
		case protocol.DeliveryEventTypeLaneSemarSubmitted:
			recordID := stringField(ev.Payload, "record_id")
			l.SemarRecordId = &recordID
			l.GarengRecordId, l.PetrukRecordId, l.BagongRecordId = nil, nil, nil
		case protocol.DeliveryEventTypeLaneGarengSubmitted:
			recordID := stringField(ev.Payload, "record_id")
			l.GarengRecordId = &recordID
			l.PetrukRecordId, l.BagongRecordId = nil, nil
		case protocol.DeliveryEventTypeLanePetrukSubmitted:
			recordID := stringField(ev.Payload, "record_id")
			l.PetrukRecordId = &recordID
			l.BagongRecordId = nil
		case protocol.DeliveryEventTypeLaneBagongSubmitted:
			recordID := stringField(ev.Payload, "record_id")
			l.BagongRecordId = &recordID
		case protocol.DeliveryEventTypeLaneVerificationDimensionRecorded,
			protocol.DeliveryEventTypeLaneCiCheckReported,
			protocol.DeliveryEventTypeLaneCommitRecorded,
			protocol.DeliveryEventTypeLaneReviewConclusionRecorded:
			// Recorded in the lane's own event log (so verification.go's
			// BuildVerificationMatrix/GetLatestReviewConclusion can scan
			// them directly and so this switch stays exhaustive), but
			// deliberately not folded into any DeliveryLane field - see
			// verification.go's package doc for why a verification matrix
			// and a lane's review conclusions are computed read-models
			// instead of accumulating lane state here.
		case protocol.DeliveryEventTypeLanePrPublished:
			// PR identity is 1:1-per-lane durable state other code needs to
			// read cheaply without a full event scan, the same precedent as
			// branch/worktree_path/base_sha above - unlike verification and
			// review conclusions, which are naturally multi-valued and stay
			// off this struct. Publishing a PR is orthogonal to a lane's
			// lease/lifecycle status, so status is left untouched here.
			provider := protocol.DeliveryLanePrProvider(stringField(ev.Payload, "provider"))
			repoSlug := stringField(ev.Payload, "repo_slug")
			number := int(numberField(ev.Payload, "number"))
			url := stringField(ev.Payload, "url")
			l.PrProvider = &provider
			l.PrRepoSlug = &repoSlug
			l.PrNumber = &number
			l.PrUrl = &url
		case protocol.DeliveryEventTypeLaneRepairCycleStarted:
			count := 0
			if l.RepairCycleCount != nil {
				count = *l.RepairCycleCount
			}
			count++
			l.RepairCycleCount = &count
			l.Status = protocol.DeliveryLaneStatusRunnable
		case protocol.DeliveryEventTypeLaneEscalated:
			// A human must look at this attempt; escalation never changes
			// whatever status the lane is currently in.
			escalatedAt := ev.OccurredAt
			l.EscalatedAt = &escalatedAt
		default:
			return nil, fmt.Errorf("delivery: unknown lane event type %q", ev.Type)
		}
	}
	return l, nil
}

// allLanes reduces every lane entity in events into its current
// DeliveryLane state, keyed by lane id.
func allLanes(orchestrationID string, events []protocol.DeliveryEvent) (map[string]*protocol.DeliveryLane, error) {
	ids := map[string]bool{}
	for _, ev := range events {
		if ev.Type == protocol.DeliveryEventTypeLaneCreated && ev.EntityId != nil {
			ids[*ev.EntityId] = true
		}
	}
	out := make(map[string]*protocol.DeliveryLane, len(ids))
	for id := range ids {
		l, err := reduceLane(orchestrationID, id, events)
		if err != nil {
			return nil, err
		}
		out[id] = l
	}
	return out, nil
}

func stringSliceField(payload protocol.DeliveryEventPayload, key string) []string {
	raw, ok := payload[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// reduceRequirementSource derives one RequirementSource's current state
// from the requirement.captured events scoped to sourceID. Later events
// overwrite content fields (a re-capture with changed content); the
// first event fixes id/provider/parent_source_id, which do not change
// across re-captures.
func reduceRequirementSource(orchestrationID, sourceID string, events []protocol.DeliveryEvent) (*protocol.RequirementSource, error) {
	var own []protocol.DeliveryEvent
	for _, ev := range events {
		if ev.EntityId != nil && *ev.EntityId == sourceID {
			own = append(own, ev)
		}
	}
	if len(own) == 0 {
		return nil, ErrNotFound
	}
	if own[0].Type != protocol.DeliveryEventTypeRequirementCaptured {
		return nil, fmt.Errorf("delivery: requirement source %s event log does not start with requirement.captured", sourceID)
	}

	s := &protocol.RequirementSource{Id: sourceID, OrchestrationId: orchestrationID, CapturedAt: own[0].OccurredAt}
	for _, ev := range own {
		if ev.Type != protocol.DeliveryEventTypeRequirementCaptured {
			return nil, fmt.Errorf("delivery: unknown requirement source event type %q", ev.Type)
		}
		s.Revision++
		s.Provider = protocol.RequirementSourceProvider(stringField(ev.Payload, "provider"))
		s.CanonicalKey = stringField(ev.Payload, "canonical_key")
		s.ContentHash = stringField(ev.Payload, "content_hash")
		s.Title = stringField(ev.Payload, "title")
		if v := stringField(ev.Payload, "external_id"); v != "" {
			s.ExternalId = &v
		}
		if v := stringField(ev.Payload, "summary"); v != "" {
			s.Summary = &v
		}
		if v := stringField(ev.Payload, "parent_source_id"); v != "" {
			s.ParentSourceId = &v
		}
	}
	return s, nil
}

// allRequirementSources reduces every requirement.captured entity in
// events into its current RequirementSource state, keyed by source id.
func allRequirementSources(orchestrationID string, events []protocol.DeliveryEvent) (map[string]*protocol.RequirementSource, error) {
	ids := map[string]bool{}
	for _, ev := range events {
		if ev.Type == protocol.DeliveryEventTypeRequirementCaptured && ev.EntityId != nil {
			ids[*ev.EntityId] = true
		}
	}
	out := make(map[string]*protocol.RequirementSource, len(ids))
	for id := range ids {
		s, err := reduceRequirementSource(orchestrationID, id, events)
		if err != nil {
			return nil, err
		}
		out[id] = s
	}
	return out, nil
}

// findByCanonicalKey returns the requirement source already captured
// under canonicalKey, or nil if none exists yet.
func findByCanonicalKey(orchestrationID, canonicalKey string, events []protocol.DeliveryEvent) (*protocol.RequirementSource, error) {
	all, err := allRequirementSources(orchestrationID, events)
	if err != nil {
		return nil, err
	}
	for _, s := range all {
		if s.CanonicalKey == canonicalKey {
			return s, nil
		}
	}
	return nil, nil
}

// reduceParentTask derives one ParentTask's current state from the
// task.created/task.routed events scoped to taskID.
func reduceParentTask(orchestrationID, taskID string, events []protocol.DeliveryEvent) (*protocol.ParentTask, error) {
	var own []protocol.DeliveryEvent
	for _, ev := range events {
		if ev.EntityId != nil && *ev.EntityId == taskID &&
			(ev.Type == protocol.DeliveryEventTypeTaskCreated || ev.Type == protocol.DeliveryEventTypeTaskRouted) {
			own = append(own, ev)
		}
	}
	if len(own) == 0 {
		return nil, ErrNotFound
	}
	if own[0].Type != protocol.DeliveryEventTypeTaskCreated {
		return nil, fmt.Errorf("delivery: task %s event log does not start with task.created", taskID)
	}

	t := &protocol.ParentTask{Id: taskID, OrchestrationId: orchestrationID, Status: protocol.ParentTaskStatusUnrouted}
	for _, ev := range own {
		t.UpdatedAt = ev.OccurredAt
		t.Revision++
		switch ev.Type {
		case protocol.DeliveryEventTypeTaskCreated:
			t.CreatedAt = ev.OccurredAt
			t.Title = stringField(ev.Payload, "title")
			if raw, ok := ev.Payload["source_ids"].([]interface{}); ok {
				for _, v := range raw {
					if s, ok := v.(string); ok {
						t.SourceIds = append(t.SourceIds, s)
					}
				}
			}
		case protocol.DeliveryEventTypeTaskRouted:
			projectID := stringField(ev.Payload, "project_id")
			t.ProjectId = &projectID
			t.Status = protocol.ParentTaskStatusRouted
		}
	}
	return t, nil
}

func allParentTasks(orchestrationID string, events []protocol.DeliveryEvent) (map[string]*protocol.ParentTask, error) {
	ids := map[string]bool{}
	for _, ev := range events {
		if ev.Type == protocol.DeliveryEventTypeTaskCreated && ev.EntityId != nil {
			ids[*ev.EntityId] = true
		}
	}
	out := make(map[string]*protocol.ParentTask, len(ids))
	for id := range ids {
		t, err := reduceParentTask(orchestrationID, id, events)
		if err != nil {
			return nil, err
		}
		out[id] = t
	}
	return out, nil
}

// reduceDependencyEdge derives one DependencyEdge's current state from
// the edge.added/edge.removed events scoped to edgeID.
func reduceDependencyEdge(orchestrationID, edgeID string, events []protocol.DeliveryEvent) (*protocol.DependencyEdge, error) {
	var own []protocol.DeliveryEvent
	for _, ev := range events {
		if ev.EntityId != nil && *ev.EntityId == edgeID &&
			(ev.Type == protocol.DeliveryEventTypeEdgeAdded || ev.Type == protocol.DeliveryEventTypeEdgeRemoved) {
			own = append(own, ev)
		}
	}
	if len(own) == 0 {
		return nil, ErrNotFound
	}
	if own[0].Type != protocol.DeliveryEventTypeEdgeAdded {
		return nil, fmt.Errorf("delivery: edge %s event log does not start with edge.added", edgeID)
	}

	e := &protocol.DependencyEdge{
		Id: edgeID, OrchestrationId: orchestrationID, CreatedAt: own[0].OccurredAt,
		FromTaskId: stringField(own[0].Payload, "from_task_id"),
		ToTaskId:   stringField(own[0].Payload, "to_task_id"),
		Type:       protocol.DependencyEdgeType(stringField(own[0].Payload, "type")),
		Origin:     protocol.DependencyEdgeOrigin(stringField(own[0].Payload, "origin")),
		Confidence: numberField(own[0].Payload, "confidence"),
		Status:     protocol.DependencyEdgeStatusActive,
	}
	if v := stringField(own[0].Payload, "evidence"); v != "" {
		e.Evidence = &v
	}
	for _, ev := range own {
		e.Revision++
		if ev.Type == protocol.DeliveryEventTypeEdgeRemoved {
			e.Status = protocol.DependencyEdgeStatusRemoved
			if v := stringField(ev.Payload, "removal_evidence"); v != "" {
				e.RemovalEvidence = &v
			}
		}
	}
	return e, nil
}

func allDependencyEdges(orchestrationID string, events []protocol.DeliveryEvent) (map[string]*protocol.DependencyEdge, error) {
	ids := map[string]bool{}
	for _, ev := range events {
		if ev.Type == protocol.DeliveryEventTypeEdgeAdded && ev.EntityId != nil {
			ids[*ev.EntityId] = true
		}
	}
	out := make(map[string]*protocol.DependencyEdge, len(ids))
	for id := range ids {
		e, err := reduceDependencyEdge(orchestrationID, id, events)
		if err != nil {
			return nil, err
		}
		out[id] = e
	}
	return out, nil
}

func stringField(payload protocol.DeliveryEventPayload, key string) string {
	v, _ := payload[key].(string)
	return v
}

// applyOptionalText folds one optional free-text field out of a payload
// into the pointer field it belongs to. A key the event never carried
// leaves the current value untouched, which is what lets one event
// change a title without disturbing a description; a key carrying only
// whitespace clears the field back to absent, which is how a caller
// takes back prose it no longer wants rather than being stuck with it
// forever.
func applyOptionalText(field **string, payload protocol.DeliveryEventPayload, key string) {
	raw, present := payload[key]
	if !present {
		return
	}
	text, _ := raw.(string)
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		*field = nil
		return
	}
	*field = &trimmed
}

// applyOptionalInt is applyOptionalText's counterpart for an integer
// field: a key the event never carried leaves the current value
// untouched, and a key carrying zero or less (never a valid revision,
// which starts at 1) clears the field back to absent. JSON numbers
// decode as float64 through the payload's map[string]interface{}, so
// that is the only shape this expects.
func applyOptionalInt(field **int, payload protocol.DeliveryEventPayload, key string) {
	raw, present := payload[key]
	if !present {
		return
	}
	n, ok := raw.(float64)
	if !ok || n <= 0 {
		*field = nil
		return
	}
	v := int(n)
	*field = &v
}

// indexOfString returns where value sits in list, or -1 when it is
// absent.
func indexOfString(list []string, value string) int {
	for i, v := range list {
		if v == value {
			return i
		}
	}
	return -1
}

func numberField(payload protocol.DeliveryEventPayload, key string) float64 {
	v, _ := payload[key].(float64)
	return v
}

func decodeUnresolvedInputs(v interface{}) ([]protocol.DeliveryOrchestrationUnresolvedInputsElem, error) {
	raw, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("delivery: unresolved_inputs payload has unexpected shape %T", v)
	}
	out := make([]protocol.DeliveryOrchestrationUnresolvedInputsElem, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("delivery: unresolved_inputs element has unexpected shape %T", item)
		}
		ref, _ := m["reference"].(string)
		elem := protocol.DeliveryOrchestrationUnresolvedInputsElem{Reference: ref}
		if note, ok := m["note"].(string); ok && note != "" {
			elem.Note = &note
		}
		out = append(out, elem)
	}
	return out, nil
}
