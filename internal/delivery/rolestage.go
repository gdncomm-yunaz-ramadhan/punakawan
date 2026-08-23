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

// roleStageName returns the workflow definition Roles map key
// corresponding to stage (semar|gareng|petruk|bagong), matching
// internal/workflowdef.RoleRestriction's map keys.
func roleStageName(stage RoleStage) string {
	switch stage {
	case RoleStageSemar:
		return "semar"
	case RoleStageGareng:
		return "gareng"
	case RoleStagePetruk:
		return "petruk"
	case RoleStageBagong:
		return "bagong"
	default:
		return ""
	}
}

// isRequired reports whether stage is required per requiredStages, a
// map of role name to its Required flag resolved from a workflow
// definition's Roles map. A stage whose name is absent from
// requiredStages - including when requiredStages is nil, which is what
// a lane with no attached definition (or no configured resolver)
// always resolves to - defaults to required, matching this package's
// behavior before workflow definitions could customize the gate. Only
// an explicit false entry turns a stage off.
func isRequired(requiredStages map[string]bool, stage RoleStage) bool {
	if v, ok := requiredStages[roleStageName(stage)]; ok {
		return v
	}
	return true
}

// recordIDForStage returns the lane's currently recorded record id for
// stage, or nil if that stage has not been recorded for the lane's
// current attempt.
func recordIDForStage(lane *protocol.DeliveryLane, stage RoleStage) *string {
	switch stage {
	case RoleStageSemar:
		return lane.SemarRecordId
	case RoleStageGareng:
		return lane.GarengRecordId
	case RoleStagePetruk:
		return lane.PetrukRecordId
	case RoleStageBagong:
		return lane.BagongRecordId
	default:
		return nil
	}
}

// precedingRecordID returns the record id the lane already has
// recorded for the nearest required stage before stage, skipping over
// any stage requiredStages marks not required, or "" if that stage has
// not been recorded yet (or stage has no required predecessor - either
// because it is the first stage, or because every stage before it was
// marked not required). With requiredStages nil or empty every stage is
// required, so this reduces to exactly stage's immediate predecessor -
// this package's behavior before workflow definitions could customize
// the gate.
func precedingRecordID(lane *protocol.DeliveryLane, stage RoleStage, requiredStages map[string]bool) string {
	for s := stage - 1; s >= RoleStageSemar; s-- {
		if !isRequired(requiredStages, s) {
			continue
		}
		id := recordIDForStage(lane, s)
		if id == nil {
			return ""
		}
		return *id
	}
	return ""
}

// lastRequiredStage returns the last (highest-order) stage requiredStages
// marks required, walking backward from Bagong to Semar, and true. It
// returns ok=false only if every one of the four stages was explicitly
// marked not required - a degenerate configuration in which the gate has
// nothing left to require.
func lastRequiredStage(requiredStages map[string]bool) (stage RoleStage, ok bool) {
	for s := RoleStageBagong; s >= RoleStageSemar; s-- {
		if isRequired(requiredStages, s) {
			return s, true
		}
	}
	return 0, false
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
		if stage != RoleStageSemar {
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
			if precedingRecordID(lane, stage, requiredStages) == "" {
				return ErrRoleStageOutOfOrder
			}
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
