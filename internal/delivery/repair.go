// repair.go implements a bounded repair loop for a lane whose review or CI
// came back lacking, and a read-only merge-readiness check against a
// project's required verification gates. Nothing here ever merges or
// closes a pull request - MergeReadiness only reports whether a lane is
// ready, it never acts on that answer.
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

// MaxRepairCycles is the most repair cycles one lane's attempt may go
// through before StartRepairCycle escalates it instead of starting another.
const MaxRepairCycles = 3

// StartRepairCycle records that laneID's current attempt needs another
// pass, kicking it back to runnable for rework - unless it has already used
// up its repair-cycle budget, in which case it is escalated instead (see
// ErrRepairCyclesExhausted) rather than looping forever.
func (s *Store) StartRepairCycle(ctx context.Context, idempotencyKey, orchestrationID, laneID, reason string, evidenceIDs []string, expectedRevision int) (*protocol.DeliveryLane, error) {
	if reason == "" {
		return nil, fmt.Errorf("delivery: repair cycle requires a non-empty reason")
	}

	lane, err := s.GetLane(ctx, orchestrationID, laneID)
	if err != nil {
		return nil, err
	}
	if lane.Revision != expectedRevision {
		return nil, ErrRevisionConflict
	}
	if lane.Status != protocol.DeliveryLaneStatusReview && lane.Status != protocol.DeliveryLaneStatusRunning {
		return nil, ErrLaneNotRunnable
	}

	cycleCount := 0
	if lane.RepairCycleCount != nil {
		cycleCount = *lane.RepairCycleCount
	}

	if cycleCount >= MaxRepairCycles {
		writeErr := s.db.Write(ctx, idempotencyKey, "escalate lane "+laneID, func(tx *sql.Tx) error {
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

			encoded, err := json.Marshal(map[string]interface{}{"reason": reason})
			if err != nil {
				return fmt.Errorf("delivery: encode lane escalated payload: %w", err)
			}
			return insertEvent(ctx, tx, eventRow{
				ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
				Type: string(protocol.DeliveryEventTypeLaneEscalated), Payload: string(encoded),
				Sequence: len(events), OccurredAt: time.Now().UTC(),
			})
		})
		if writeErr != nil && !errors.Is(writeErr, storage.ErrDuplicateWrite) {
			return nil, writeErr
		}
		reloaded, err := s.GetLane(ctx, orchestrationID, laneID)
		if err != nil {
			return nil, err
		}
		return reloaded, ErrRepairCyclesExhausted
	}

	payload := map[string]interface{}{"reason": reason, "evidence_ids": evidenceIDs}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("delivery: encode repair cycle started payload: %w", err)
	}

	writeErr := s.db.Write(ctx, idempotencyKey, "start repair cycle "+laneID, func(tx *sql.Tx) error {
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
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeLaneRepairCycleStarted), Payload: string(encoded),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if writeErr != nil && !errors.Is(writeErr, storage.ErrDuplicateWrite) {
		return nil, writeErr
	}
	return s.GetLane(ctx, orchestrationID, laneID)
}

// mergeReadinessGateNames maps every profile.VerificationGates entry that
// matches one of the six fixed verification dimensions to the matrix's own
// status for that dimension, so MergeReadiness never has to duplicate
// BuildVerificationMatrix's own dimension list.
func mergeReadinessGateNames(matrix *protocol.VerificationMatrix) map[string]protocol.VerificationMatrixDimensionsElemStatus {
	byName := make(map[string]protocol.VerificationMatrixDimensionsElemStatus, len(matrix.Dimensions))
	for _, dim := range matrix.Dimensions {
		byName[string(dim.Name)] = dim.Status
	}
	return byName
}

// MergeReadiness reports whether laneID is ready to merge against profile's
// required verification gates: every one of profile.VerificationGates must
// name a known verification dimension whose current status is passed, and
// the lane's latest review conclusion must be approved. It performs no
// mutation and never merges or closes anything - only BuildVerificationMatrix
// and GetLatestReviewConclusion, both read-only, are called.
func (s *Store) MergeReadiness(ctx context.Context, orchestrationID, laneID string, profile *protocol.ProjectDeliveryProfile) (bool, []string, error) {
	matrix, err := s.BuildVerificationMatrix(ctx, orchestrationID, laneID)
	if err != nil {
		return false, nil, err
	}
	byName := mergeReadinessGateNames(matrix)

	var failingGates []string
	for _, gate := range profile.VerificationGates {
		status, known := byName[gate]
		if !known || status != protocol.VerificationMatrixDimensionsElemStatusPassed {
			failingGates = append(failingGates, gate)
		}
	}

	conclusion, err := s.GetLatestReviewConclusion(ctx, orchestrationID, laneID)
	switch {
	case errors.Is(err, ErrNotFound):
		failingGates = append(failingGates, "review_conclusion")
	case err != nil:
		return false, nil, err
	case conclusion.Outcome != protocol.ReviewConclusionOutcomeApproved:
		failingGates = append(failingGates, "review_conclusion")
	}

	return len(failingGates) == 0, failingGates, nil
}
