// verification.go implements a lane's verification matrix and its review
// conclusions as computed, non-reduced read-models built by scanning a
// lane's own event log - the same pattern context.go's LaneContext uses -
// rather than as fields folded into DeliveryLane/reduceLane. Both are
// naturally multi-valued and accumulate over a lane's lifetime (more
// dimensions get checked, more conclusions get recorded across retries),
// unlike the four existing *_record_id role-stage fields, which are each a
// single latest-pointer.
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

// fixedVerificationDimensionNames is BuildVerificationMatrix's complete,
// ordered set of dimensions. A VerificationMatrix always carries exactly
// one entry per name here, defaulting to pending when nothing has been
// recorded or derived for it yet, so a caller never has to special-case a
// missing dimension.
var fixedVerificationDimensionNames = []protocol.VerificationMatrixDimensionsElemName{
	protocol.VerificationMatrixDimensionsElemNameLogic,
	protocol.VerificationMatrixDimensionsElemNameUnit,
	protocol.VerificationMatrixDimensionsElemNameIntegration,
	protocol.VerificationMatrixDimensionsElemNameQuality,
	protocol.VerificationMatrixDimensionsElemNameE2E,
	protocol.VerificationMatrixDimensionsElemNameCi,
}

// isLaneTerminal reports whether status is one a lane's attempt can no
// longer accept new verification evidence or review conclusions against.
// DeliveryLane's own status enum (deliverylane.schema.json) has no
// "cancelled" value distinct from "failed" - accepted and failed are its
// only two terminal states.
func isLaneTerminal(status protocol.DeliveryLaneStatus) bool {
	return status == protocol.DeliveryLaneStatusAccepted || status == protocol.DeliveryLaneStatusFailed
}

// RecordVerificationDimension appends lane.verification_dimension_recorded
// for laneID's current attempt. Any connected agent or provider adapter may
// call this - Punakawan never reasons about whether a dimension actually
// passed, it only persists what the caller reports - so there is no
// lease-token check and no fixed ordering between dimensions, unlike the
// four role stages in rolestage.go.
func (s *Store) RecordVerificationDimension(ctx context.Context, idempotencyKey, orchestrationID, laneID string, name protocol.VerificationDimensionName, status protocol.VerificationDimensionStatus, evidenceID, summary string, expectedRevision int) error {
	err := s.db.Write(ctx, idempotencyKey, "record verification dimension "+laneID, func(tx *sql.Tx) error {
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
		if isLaneTerminal(lane.Status) {
			return ErrLaneTerminal
		}

		payload := map[string]interface{}{"name": string(name), "status": string(status)}
		if evidenceID != "" {
			payload["evidence_id"] = evidenceID
		}
		if summary != "" {
			payload["summary"] = summary
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("delivery: encode verification dimension payload: %w", err)
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeLaneVerificationDimensionRecorded), Payload: string(encoded),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return err
	}
	return nil
}

// RecordCICheck appends lane.ci_check_reported for laneID's current
// attempt. Like RecordVerificationDimension, any caller may report a CI
// check (a human, a role, or an external CI-adapter poller) - there is no
// lease-token check.
func (s *Store) RecordCICheck(ctx context.Context, idempotencyKey, orchestrationID, laneID string, check protocol.CICheck, expectedRevision int) error {
	err := s.db.Write(ctx, idempotencyKey, "record ci check "+laneID, func(tx *sql.Tx) error {
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
		if isLaneTerminal(lane.Status) {
			return ErrLaneTerminal
		}

		encoded, err := json.Marshal(check)
		if err != nil {
			return fmt.Errorf("delivery: encode ci check payload: %w", err)
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeLaneCiCheckReported), Payload: string(encoded),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return err
	}
	return nil
}

// ciCheckState is the latest known status of one CI check, keyed by its
// external_id while folding a lane's lane.ci_check_reported events.
type ciCheckState struct {
	required bool
	status   protocol.CICheckStatus
}

// deriveCIDimensionStatus computes the ci dimension's status from the
// latest per-check state folded from a lane's CI check reports: passed
// only if every required check's latest status is passed, failed if any
// required check's latest status is failed or cancelled, pending
// otherwise (including when there are no required checks at all - a lane
// with no CI check reports yet has nothing to derive a pass or fail from).
func deriveCIDimensionStatus(checks map[string]ciCheckState) protocol.VerificationMatrixDimensionsElemStatus {
	hasRequired := false
	for _, c := range checks {
		if !c.required {
			continue
		}
		hasRequired = true
		if c.status == protocol.CICheckStatusFailed || c.status == protocol.CICheckStatusCancelled {
			return protocol.VerificationMatrixDimensionsElemStatusFailed
		}
	}
	if !hasRequired {
		return protocol.VerificationMatrixDimensionsElemStatusPending
	}
	for _, c := range checks {
		if c.required && c.status != protocol.CICheckStatusPassed {
			return protocol.VerificationMatrixDimensionsElemStatusPending
		}
	}
	return protocol.VerificationMatrixDimensionsElemStatusPassed
}

// explicitDimension is one dimension's latest explicitly-recorded state,
// folded from a lane's lane.verification_dimension_recorded events.
type explicitDimension struct {
	status     protocol.VerificationMatrixDimensionsElemStatus
	evidenceID *string
	summary    *string
	checkedAt  time.Time
}

// BuildVerificationMatrix folds laneID's own lane.verification_dimension_recorded
// and lane.ci_check_reported events into its current VerificationMatrix. Every
// one of the six fixed dimensions is always present, even when nothing has
// ever been recorded for it (defaulting to pending, with no evidence_id or
// summary) - a caller never has to special-case a missing dimension.
func (s *Store) BuildVerificationMatrix(ctx context.Context, orchestrationID, laneID string) (*protocol.VerificationMatrix, error) {
	events, err := loadEvents(ctx, s.db.Reader(), orchestrationID)
	if err != nil {
		return nil, err
	}
	lane, err := reduceLane(orchestrationID, laneID, events)
	if err != nil {
		return nil, err
	}

	var laneEvents []protocol.DeliveryEvent
	for _, ev := range events {
		if ev.EntityId != nil && *ev.EntityId == laneID {
			laneEvents = append(laneEvents, ev)
		}
	}

	explicit := map[protocol.VerificationMatrixDimensionsElemName]explicitDimension{}
	ciChecks := map[string]ciCheckState{}

	for _, ev := range laneEvents {
		switch ev.Type {
		case protocol.DeliveryEventTypeLaneVerificationDimensionRecorded:
			name := protocol.VerificationMatrixDimensionsElemName(stringField(ev.Payload, "name"))
			dim := explicitDimension{
				status:    protocol.VerificationMatrixDimensionsElemStatus(stringField(ev.Payload, "status")),
				checkedAt: ev.OccurredAt,
			}
			if v := stringField(ev.Payload, "evidence_id"); v != "" {
				dim.evidenceID = &v
			}
			if v := stringField(ev.Payload, "summary"); v != "" {
				dim.summary = &v
			}
			explicit[name] = dim
		case protocol.DeliveryEventTypeLaneCiCheckReported:
			check, err := decodeCICheck(ev.Payload)
			if err != nil {
				return nil, err
			}
			ciChecks[check.ExternalId] = ciCheckState{required: check.Required, status: check.Status}
		}
	}

	// computed_at is derived entirely from already-recorded event data
	// (the last lane event's occurred_at, or the lane's own created_at if
	// it has no events at all) rather than wall-clock time, so replaying
	// the exact same event log always computes the exact same matrix.
	computedAt := lane.CreatedAt
	if len(laneEvents) > 0 {
		computedAt = laneEvents[len(laneEvents)-1].OccurredAt
	}

	dimensions := make([]protocol.VerificationMatrixDimensionsElem, 0, len(fixedVerificationDimensionNames))
	for _, name := range fixedVerificationDimensionNames {
		if dim, ok := explicit[name]; ok {
			elem := protocol.VerificationMatrixDimensionsElem{Name: name, Status: dim.status}
			elem.EvidenceId = dim.evidenceID
			elem.Summary = dim.summary
			if !dim.checkedAt.IsZero() {
				checkedAt := dim.checkedAt
				elem.CheckedAt = &checkedAt
			}
			dimensions = append(dimensions, elem)
			continue
		}
		if name == protocol.VerificationMatrixDimensionsElemNameCi {
			// No explicit lane.verification_dimension_recorded event for
			// ci exists - derive it from folded CI check reports instead.
			// A lane with no CI check reports either falls through to the
			// same pending default every other unreported dimension gets.
			dimensions = append(dimensions, protocol.VerificationMatrixDimensionsElem{
				Name:   name,
				Status: deriveCIDimensionStatus(ciChecks),
			})
			continue
		}
		dimensions = append(dimensions, protocol.VerificationMatrixDimensionsElem{
			Name:   name,
			Status: protocol.VerificationMatrixDimensionsElemStatusPending,
		})
	}

	return &protocol.VerificationMatrix{
		LaneId:          laneID,
		OrchestrationId: orchestrationID,
		Dimensions:      dimensions,
		ComputedAt:      computedAt,
	}, nil
}

// GetLatestReviewConclusion returns laneID's most recently recorded review
// conclusion, or ErrNotFound if none has been recorded for this attempt.
func (s *Store) GetLatestReviewConclusion(ctx context.Context, orchestrationID, laneID string) (*protocol.ReviewConclusion, error) {
	events, err := loadEvents(ctx, s.db.Reader(), orchestrationID)
	if err != nil {
		return nil, err
	}
	if _, err := reduceLane(orchestrationID, laneID, events); err != nil {
		return nil, err
	}

	var latest *protocol.ReviewConclusion
	for _, ev := range events {
		if ev.EntityId == nil || *ev.EntityId != laneID || ev.Type != protocol.DeliveryEventTypeLaneReviewConclusionRecorded {
			continue
		}
		conclusion, err := decodeReviewConclusion(ev.Payload)
		if err != nil {
			return nil, err
		}
		latest = conclusion
	}
	if latest == nil {
		return nil, ErrNotFound
	}
	return latest, nil
}

// RecordReviewConclusion appends lane.review_conclusion_recorded for
// laneID's current attempt, after validating that the reported conclusion
// is well-formed and independent of implementerSessionID. A conclusion
// whose reviewer_session_id equals implementerSessionID - the same
// session that implemented the attempt being reviewed - is rejected with
// ErrIndependenceRequired unless conclusion.IndependenceOverrideReason is
// set, in which case it is allowed through unchanged (no extra logging;
// the recorded conclusion itself carries the override reason as its own
// audit trail).
func (s *Store) RecordReviewConclusion(ctx context.Context, idempotencyKey, orchestrationID, laneID string, conclusion protocol.ReviewConclusion, implementerSessionID string, expectedRevision int) (*protocol.ReviewConclusion, error) {
	if conclusion.ReviewerWorkerId == "" {
		return nil, fmt.Errorf("delivery: review conclusion requires reviewer_worker_id")
	}
	if conclusion.ReviewerSessionId == "" {
		return nil, fmt.Errorf("delivery: review conclusion requires reviewer_session_id")
	}
	if !validReviewOutcome(conclusion.Outcome) {
		return nil, fmt.Errorf("delivery: review conclusion has invalid outcome %q", conclusion.Outcome)
	}
	overridden := conclusion.IndependenceOverrideReason != nil && *conclusion.IndependenceOverrideReason != ""
	if conclusion.ReviewerSessionId == implementerSessionID && !overridden {
		return nil, ErrIndependenceRequired
	}

	now := time.Now().UTC()
	stored := conclusion
	stored.Id = NewID()
	stored.LaneId = laneID
	stored.RecordedAt = now

	err := s.db.Write(ctx, idempotencyKey, "record review conclusion "+laneID, func(tx *sql.Tx) error {
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
		if isLaneTerminal(lane.Status) {
			return ErrLaneTerminal
		}

		encoded, err := json.Marshal(stored)
		if err != nil {
			return fmt.Errorf("delivery: encode review conclusion payload: %w", err)
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeLaneReviewConclusionRecorded), Payload: string(encoded),
			Sequence: len(events), OccurredAt: now,
		})
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetLatestReviewConclusion(ctx, orchestrationID, laneID)
}

func validReviewOutcome(outcome protocol.ReviewConclusionOutcome) bool {
	switch outcome {
	case protocol.ReviewConclusionOutcomeApproved, protocol.ReviewConclusionOutcomeChangesRequested, protocol.ReviewConclusionOutcomeBlocked:
		return true
	default:
		return false
	}
}

// decodeCICheck reconstructs a protocol.CICheck from an already-decoded
// event payload by round-tripping it through JSON - simpler and less
// error-prone than re-extracting each field by hand, and safe here
// because RecordCICheck always writes a complete, schema-valid CICheck as
// its payload.
func decodeCICheck(payload protocol.DeliveryEventPayload) (*protocol.CICheck, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("delivery: encode ci check event payload: %w", err)
	}
	var check protocol.CICheck
	if err := json.Unmarshal(encoded, &check); err != nil {
		return nil, fmt.Errorf("delivery: decode ci check event payload: %w", err)
	}
	return &check, nil
}

// decodeReviewConclusion reconstructs a protocol.ReviewConclusion from an
// already-decoded event payload the same way decodeCICheck does.
func decodeReviewConclusion(payload protocol.DeliveryEventPayload) (*protocol.ReviewConclusion, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("delivery: encode review conclusion event payload: %w", err)
	}
	var conclusion protocol.ReviewConclusion
	if err := json.Unmarshal(encoded, &conclusion); err != nil {
		return nil, fmt.Errorf("delivery: decode review conclusion event payload: %w", err)
	}
	return &conclusion, nil
}
