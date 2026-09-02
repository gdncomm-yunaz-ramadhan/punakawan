package delivery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// CompleteLaneWork drives a lane all the way from runnable to a terminal
// status in one call.
//
// Every step it performs already existed - GrantLease,
// RecordVerificationDimension, CompleteLease, RecordReviewConclusion,
// UpdateLaneStatus - but none of them was reachable from the public tool
// surface, so a lane created by start_delivery was structurally stuck at
// runnable with all six verification dimensions pending, and a delivery
// could be completed on top of it without anything noticing.
//
// The steps are deliberately composed here rather than exposed
// individually. A lease is bookkeeping the orchestrator owns: an agent
// that has just finished a piece of work should say so and say what it
// verified, not acquire a lease, heartbeat it, and hand back a token.
//
// This is not atomic. Each step is its own durable event, and a failure
// part-way through leaves the lane wherever it got to - which is the
// honest record of what happened, and is recoverable by calling again
// with the lane's current revision. Derived idempotency keys mean a
// retry with the same base key replays rather than re-appends.
type LaneCompletionRequest struct {
	IdempotencyKey  string
	OrchestrationID string
	LaneID          string
	// ExpectedRevision is the lane's revision as the caller last saw it,
	// checked before the first transition. Later steps use the revision
	// each preceding step produced, so the caller never has to predict
	// how many events its own request will append.
	ExpectedRevision int
	// WorkerID and SessionID identify who did the work. SessionID is also
	// the implementer identity the review-independence check runs against.
	WorkerID  string
	SessionID string
	// Verifications is what the caller observed. Punakawan never decides
	// whether a dimension passed; it records what it is told.
	Verifications []LaneVerification
	// Outcome must be accepted or failed - the lane status enum's only
	// two terminal values.
	Outcome protocol.DeliveryLaneStatus
	Summary string
	// IndependenceOverrideReason lets the session that implemented the
	// work also conclude the review. A single-agent delivery has no
	// separate reviewer, and without this it could never close a lane at
	// all; requiring a stated reason keeps that visible in the audit
	// trail rather than silent.
	IndependenceOverrideReason string
}

// LaneVerification is one reported verification dimension outcome.
type LaneVerification struct {
	Name     protocol.VerificationDimensionName
	Status   protocol.VerificationDimensionStatus
	Summary  string
	Evidence string
}

func (r LaneCompletionRequest) validate() error {
	if strings.TrimSpace(r.OrchestrationID) == "" || strings.TrimSpace(r.LaneID) == "" {
		return fmt.Errorf("delivery: complete lane work requires orchestration_id and lane_id")
	}
	if strings.TrimSpace(r.WorkerID) == "" || strings.TrimSpace(r.SessionID) == "" {
		return fmt.Errorf("delivery: complete lane work requires worker_id and session_id")
	}
	if strings.TrimSpace(r.Summary) == "" {
		return fmt.Errorf("delivery: complete lane work requires a summary of what was done")
	}
	if r.Outcome != protocol.DeliveryLaneStatusAccepted && r.Outcome != protocol.DeliveryLaneStatusFailed {
		return fmt.Errorf("delivery: complete lane work outcome must be accepted or failed, got %q", r.Outcome)
	}
	for _, v := range r.Verifications {
		if !validVerificationDimensionName(v.Name) {
			return fmt.Errorf("delivery: unknown verification dimension %q", v.Name)
		}
		if v.Status != protocol.VerificationDimensionStatusPassed &&
			v.Status != protocol.VerificationDimensionStatusFailed &&
			v.Status != protocol.VerificationDimensionStatusPending {
			return fmt.Errorf("delivery: verification dimension %q has invalid status %q", v.Name, v.Status)
		}
	}
	return nil
}

func validVerificationDimensionName(name protocol.VerificationDimensionName) bool {
	for _, known := range fixedVerificationDimensionNames {
		if string(known) == string(name) {
			return true
		}
	}
	return false
}

// laneCompletionLeaseDuration only has to outlive this call's own
// remaining steps - the lease exists so CompleteLease has a token to
// check, not so a worker can hold the lane across requests.
const laneCompletionLeaseDuration = 5 * time.Minute

func (s *Store) CompleteLaneWork(ctx context.Context, req LaneCompletionRequest) (*protocol.DeliveryLane, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	base := strings.TrimSpace(req.IdempotencyKey)
	if base == "" {
		base = NewID()
	}
	key := func(step string) string { return base + ":" + step }

	lane, err := s.GetLane(ctx, req.OrchestrationID, req.LaneID)
	if err != nil {
		return nil, err
	}
	if lane.Revision != req.ExpectedRevision {
		return nil, ErrRevisionConflict
	}
	if isLaneTerminal(lane.Status) {
		return nil, ErrLaneTerminal
	}

	if lane.Status == protocol.DeliveryLaneStatusRunnable {
		lane, err = s.GrantLease(ctx, key("lease"), req.OrchestrationID, req.LaneID, lane.Revision, req.WorkerID, laneCompletionLeaseDuration)
		if err != nil {
			return nil, err
		}
	}
	if lane.LeaseToken == nil {
		// Reachable when the lane was waiting or blocked rather than
		// runnable: there is no lease to complete, and silently inventing
		// one would report work against a lane the frontier never opened.
		return nil, ErrLaneNotRunnable
	}
	leaseToken := *lane.LeaseToken

	// Recorded before the lane leaves the lease, because
	// RecordVerificationDimension refuses a terminal lane and a reviewer
	// is supposed to be looking at a matrix that is already populated.
	for _, v := range req.Verifications {
		if err := s.RecordVerificationDimension(ctx, key("verify:"+string(v.Name)), req.OrchestrationID, req.LaneID,
			v.Name, v.Status, v.Evidence, v.Summary, lane.Revision); err != nil {
			return nil, err
		}
		if lane, err = s.GetLane(ctx, req.OrchestrationID, req.LaneID); err != nil {
			return nil, err
		}
	}

	if lane, err = s.CompleteLease(ctx, key("complete-lease"), req.OrchestrationID, req.LaneID, leaseToken, lane.Revision); err != nil {
		return nil, err
	}

	matrix, err := s.BuildVerificationMatrix(ctx, req.OrchestrationID, req.LaneID)
	if err != nil {
		return nil, err
	}

	conclusion := protocol.ReviewConclusion{
		BlockingFindingIds:           []string{},
		EvidenceIds:                  []string{},
		IndependenceLevel:            protocol.ReviewConclusionIndependenceLevelSameSession,
		Outcome:                      protocol.ReviewConclusionOutcomeApproved,
		ReviewerSessionId:            req.SessionID,
		ReviewerWorkerId:             req.WorkerID,
		VerificationMatrixComputedAt: matrix.ComputedAt,
	}
	if req.Outcome == protocol.DeliveryLaneStatusFailed {
		conclusion.Outcome = protocol.ReviewConclusionOutcomeBlocked
	}
	if reason := strings.TrimSpace(req.IndependenceOverrideReason); reason != "" {
		conclusion.IndependenceOverrideReason = &reason
	}
	if _, err := s.RecordReviewConclusion(ctx, key("review"), req.OrchestrationID, req.LaneID, conclusion, req.SessionID, lane.Revision); err != nil {
		return nil, err
	}
	if lane, err = s.GetLane(ctx, req.OrchestrationID, req.LaneID); err != nil {
		return nil, err
	}

	return s.UpdateLaneStatus(ctx, key("terminal"), req.OrchestrationID, req.LaneID, lane.Revision, req.Outcome)
}
