package delivery

import (
	"fmt"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// reduceOrchestration derives an orchestration's current state by
// replaying its event log in sequence order. It is a pure function of
// events: the same ordered log always produces the same state, which is
// what makes replay deterministic (punokawan-14yn.1 acceptance
// criterion 4).
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
		if ev.LaneId != nil {
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
		if ev.LaneId != nil && *ev.LaneId == laneID {
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
		Status:          protocol.DeliveryLaneStatusPending,
		CreatedAt:       laneEvents[0].OccurredAt,
	}
	if parentTaskID, ok := laneEvents[0].Payload["parent_task_id"].(string); ok && parentTaskID != "" {
		l.ParentTaskId = &parentTaskID
	}

	for _, ev := range laneEvents {
		l.UpdatedAt = ev.OccurredAt
		l.Revision++
		switch ev.Type {
		case protocol.DeliveryEventTypeLaneCreated:
			// fields already applied above from laneEvents[0]
		case protocol.DeliveryEventTypeLaneStatusChanged:
			status, _ := ev.Payload["status"].(string)
			l.Status = protocol.DeliveryLaneStatus(status)
		default:
			return nil, fmt.Errorf("delivery: unknown lane event type %q", ev.Type)
		}
	}
	return l, nil
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
