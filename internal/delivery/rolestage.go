// rolestage.go tracks the one fixed order every lane's attempt
// advances through: Semar synthesizes intent, Gareng reviews
// feasibility, Petruk plans and implements, Bagong reviews
// independently. This package never invokes a role itself - a
// connected agent supplies the actual reasoning and content elsewhere,
// then presents just the resulting record id here. What this file
// enforces is structural: a stage can only be recorded once the stage
// immediately before it exists, only by whoever currently holds the
// lane's lease, and recording a stage invalidates every later stage
// already recorded for this attempt, since those were built against a
// predecessor that no longer holds.
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

// RoleStage identifies one of the four stages a lane's attempt moves
// through, in fixed order.
type RoleStage int

const (
	RoleStageSemar RoleStage = iota
	RoleStageGareng
	RoleStagePetruk
	RoleStageBagong
)

func (stage RoleStage) eventType() (protocol.DeliveryEventType, error) {
	switch stage {
	case RoleStageSemar:
		return protocol.DeliveryEventTypeLaneSemarSubmitted, nil
	case RoleStageGareng:
		return protocol.DeliveryEventTypeLaneGarengSubmitted, nil
	case RoleStagePetruk:
		return protocol.DeliveryEventTypeLanePetrukSubmitted, nil
	case RoleStageBagong:
		return protocol.DeliveryEventTypeLaneBagongSubmitted, nil
	default:
		return "", fmt.Errorf("delivery: unknown role stage %d", stage)
	}
}

// precedingRecordID returns the record id the lane already has
// recorded for the stage immediately before stage, or "" if that
// predecessor has not been recorded yet (or stage is the first stage,
// which has no predecessor to check).
func precedingRecordID(lane *protocol.DeliveryLane, stage RoleStage) string {
	var id *string
	switch stage {
	case RoleStageGareng:
		id = lane.SemarRecordId
	case RoleStagePetruk:
		id = lane.GarengRecordId
	case RoleStageBagong:
		id = lane.PetrukRecordId
	}
	if id == nil {
		return ""
	}
	return *id
}

// RecordRoleStage records recordID as laneID's current attempt's
// output for stage. Requires leaseToken to match the lane's current
// lease - only the worker holding this lane's lease may advance its
// stages - and that stage is not being recorded ahead of the stage
// immediately preceding it (ErrRoleStageOutOfOrder otherwise). Semar
// has no predecessor to check. A held lease cannot be completed via
// CompleteLease until Bagong's stage has been recorded.
func (s *Store) RecordRoleStage(ctx context.Context, idempotencyKey, orchestrationID, laneID, leaseToken string, stage RoleStage, recordID string, expectedRevision int) (*protocol.DeliveryLane, error) {
	eventType, err := stage.eventType()
	if err != nil {
		return nil, err
	}

	writeErr := s.db.Write(ctx, idempotencyKey, "record role stage "+laneID, func(tx *sql.Tx) error {
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
		if stage != RoleStageSemar && precedingRecordID(lane, stage) == "" {
			return ErrRoleStageOutOfOrder
		}

		payload, err := json.Marshal(map[string]interface{}{"record_id": recordID})
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(eventType), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if writeErr != nil && !errors.Is(writeErr, storage.ErrDuplicateWrite) {
		return nil, writeErr
	}
	return s.GetLane(ctx, orchestrationID, laneID)
}
