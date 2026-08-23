package delivery

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// laneAtReviewForRepair seeds a leased lane and completes its lease,
// leaving it at status review - the precondition StartRepairCycle checks -
// without engaging the role-stage flow at all (a lane that never opts into
// it completes exactly like any other, per rolestage_test.go).
func laneAtReviewForRepair(t *testing.T) (*Store, string, string) {
	t.Helper()
	s, orchID, laneID, token := leasedLaneForRoleStages(t)
	ctx := context.Background()
	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	completed, err := s.CompleteLease(ctx, "complete-1", orchID, laneID, token, lane.Revision)
	if err != nil {
		t.Fatalf("CompleteLease: %v", err)
	}
	if completed.Status != protocol.DeliveryLaneStatusReview {
		t.Fatalf("expected lane at review, got %s", completed.Status)
	}
	return s, orchID, laneID
}

func TestStartRepairCycleIncrementsCountAndReturnsToRunnable(t *testing.T) {
	s, orchID, laneID := laneAtReviewForRepair(t)
	ctx := context.Background()

	for i := 1; i <= MaxRepairCycles; i++ {
		lane, err := s.GetLane(ctx, orchID, laneID)
		if err != nil {
			t.Fatalf("GetLane (cycle %d): %v", i, err)
		}
		repaired, err := s.StartRepairCycle(ctx, fmt.Sprintf("repair-%d", i), orchID, laneID, "review found issues", []string{"evidence-1"}, lane.Revision)
		if err != nil {
			t.Fatalf("StartRepairCycle (cycle %d): %v", i, err)
		}
		if repaired.RepairCycleCount == nil || *repaired.RepairCycleCount != i {
			t.Fatalf("expected repair_cycle_count %d, got %+v", i, repaired.RepairCycleCount)
		}
		if repaired.Status != protocol.DeliveryLaneStatusRunnable {
			t.Fatalf("expected status runnable after repair cycle %d, got %s", i, repaired.Status)
		}

		// Move the lane back to review so the next repair cycle's
		// precondition holds, mirroring a real rework-and-resubmit pass.
		if _, err := s.UpdateLaneStatus(ctx, fmt.Sprintf("back-to-review-%d", i), orchID, laneID, repaired.Revision, protocol.DeliveryLaneStatusReview); err != nil {
			t.Fatalf("UpdateLaneStatus back to review (cycle %d): %v", i, err)
		}
	}
}

func TestStartRepairCycleEscalatesAfterBudgetExhausted(t *testing.T) {
	s, orchID, laneID := laneAtReviewForRepair(t)
	ctx := context.Background()

	for i := 1; i <= MaxRepairCycles; i++ {
		lane, err := s.GetLane(ctx, orchID, laneID)
		if err != nil {
			t.Fatalf("GetLane (cycle %d): %v", i, err)
		}
		repaired, err := s.StartRepairCycle(ctx, fmt.Sprintf("repair-%d", i), orchID, laneID, "review found issues", nil, lane.Revision)
		if err != nil {
			t.Fatalf("StartRepairCycle (cycle %d): %v", i, err)
		}
		if _, err := s.UpdateLaneStatus(ctx, fmt.Sprintf("back-to-review-%d", i), orchID, laneID, repaired.Revision, protocol.DeliveryLaneStatusReview); err != nil {
			t.Fatalf("UpdateLaneStatus back to review (cycle %d): %v", i, err)
		}
	}

	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane before exhausted call: %v", err)
	}
	escalated, err := s.StartRepairCycle(ctx, "repair-exhausted", orchID, laneID, "still broken after three attempts", nil, lane.Revision)
	if !errors.Is(err, ErrRepairCyclesExhausted) {
		t.Fatalf("expected ErrRepairCyclesExhausted on the fourth call, got %v", err)
	}
	if escalated == nil {
		t.Fatal("expected a reloaded lane alongside ErrRepairCyclesExhausted")
	}
	if escalated.EscalatedAt == nil {
		t.Fatal("expected escalated_at to be set")
	}
	if escalated.RepairCycleCount == nil || *escalated.RepairCycleCount != MaxRepairCycles {
		t.Fatalf("expected repair_cycle_count to stay at %d (escalation is not itself a repair cycle), got %+v", MaxRepairCycles, escalated.RepairCycleCount)
	}
}

func TestStartRepairCycleRejectsEmptyReason(t *testing.T) {
	s, orchID, laneID := laneAtReviewForRepair(t)
	ctx := context.Background()
	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}

	if _, err := s.StartRepairCycle(ctx, "repair-empty", orchID, laneID, "", nil, lane.Revision); err == nil {
		t.Fatal("expected an error for an empty reason")
	}
}

func TestMergeReadiness(t *testing.T) {
	ctx := context.Background()

	t.Run("no gates required and no review conclusion is not ready", func(t *testing.T) {
		s, orchID, laneID := newVerificationTestLane(t)
		profile := &protocol.ProjectDeliveryProfile{}

		ready, failing, err := s.MergeReadiness(ctx, orchID, laneID, profile)
		if err != nil {
			t.Fatalf("MergeReadiness: %v", err)
		}
		if ready {
			t.Fatal("expected not ready with no review conclusion recorded")
		}
		if len(failing) != 1 || failing[0] != "review_conclusion" {
			t.Fatalf("expected failing gates to be exactly [review_conclusion], got %v", failing)
		}
	})

	t.Run("all required gates passed and an approved conclusion is ready", func(t *testing.T) {
		s, orchID, laneID := newVerificationTestLane(t)
		lane, err := s.GetLane(ctx, orchID, laneID)
		if err != nil {
			t.Fatalf("GetLane: %v", err)
		}
		if err := s.RecordVerificationDimension(ctx, "vd-1", orchID, laneID, protocol.VerificationDimensionNameUnit, protocol.VerificationDimensionStatusPassed, "ev-1", "", lane.Revision); err != nil {
			t.Fatalf("RecordVerificationDimension: %v", err)
		}
		lane, err = s.GetLane(ctx, orchID, laneID)
		if err != nil {
			t.Fatalf("GetLane: %v", err)
		}
		conclusion := baseReviewConclusion()
		conclusion.ReviewerSessionId = "session-reviewer"
		conclusion.IndependenceLevel = protocol.ReviewConclusionIndependenceLevelDifferentSession
		if _, err := s.RecordReviewConclusion(ctx, "rc-1", orchID, laneID, conclusion, "session-implementer", lane.Revision); err != nil {
			t.Fatalf("RecordReviewConclusion: %v", err)
		}

		profile := &protocol.ProjectDeliveryProfile{VerificationGates: []string{"unit"}}
		ready, failing, err := s.MergeReadiness(ctx, orchID, laneID, profile)
		if err != nil {
			t.Fatalf("MergeReadiness: %v", err)
		}
		if !ready {
			t.Fatalf("expected ready, got failing gates %v", failing)
		}
		if len(failing) != 0 {
			t.Fatalf("expected no failing gates, got %v", failing)
		}
	})

	t.Run("one required gate still pending is not ready", func(t *testing.T) {
		s, orchID, laneID := newVerificationTestLane(t)
		lane, err := s.GetLane(ctx, orchID, laneID)
		if err != nil {
			t.Fatalf("GetLane: %v", err)
		}
		conclusion := baseReviewConclusion()
		conclusion.ReviewerSessionId = "session-reviewer"
		conclusion.IndependenceLevel = protocol.ReviewConclusionIndependenceLevelDifferentSession
		if _, err := s.RecordReviewConclusion(ctx, "rc-1", orchID, laneID, conclusion, "session-implementer", lane.Revision); err != nil {
			t.Fatalf("RecordReviewConclusion: %v", err)
		}

		profile := &protocol.ProjectDeliveryProfile{VerificationGates: []string{"unit"}}
		ready, failing, err := s.MergeReadiness(ctx, orchID, laneID, profile)
		if err != nil {
			t.Fatalf("MergeReadiness: %v", err)
		}
		if ready {
			t.Fatal("expected not ready with unit still pending")
		}
		if len(failing) != 1 || failing[0] != "unit" {
			t.Fatalf("expected failing gates to be exactly [unit], got %v", failing)
		}
	})

	t.Run("a nonexistent gate name is reported failing", func(t *testing.T) {
		s, orchID, laneID := newVerificationTestLane(t)
		lane, err := s.GetLane(ctx, orchID, laneID)
		if err != nil {
			t.Fatalf("GetLane: %v", err)
		}
		conclusion := baseReviewConclusion()
		conclusion.ReviewerSessionId = "session-reviewer"
		conclusion.IndependenceLevel = protocol.ReviewConclusionIndependenceLevelDifferentSession
		if _, err := s.RecordReviewConclusion(ctx, "rc-1", orchID, laneID, conclusion, "session-implementer", lane.Revision); err != nil {
			t.Fatalf("RecordReviewConclusion: %v", err)
		}

		profile := &protocol.ProjectDeliveryProfile{VerificationGates: []string{"performance"}}
		ready, failing, err := s.MergeReadiness(ctx, orchID, laneID, profile)
		if err != nil {
			t.Fatalf("MergeReadiness: %v", err)
		}
		if ready {
			t.Fatal("expected not ready with an unrecognized gate name")
		}
		if len(failing) != 1 || failing[0] != "performance" {
			t.Fatalf("expected failing gates to be exactly [performance], got %v", failing)
		}
	})
}
