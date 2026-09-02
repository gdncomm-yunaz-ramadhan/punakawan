package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// newRunnableTestLane is newVerificationTestLane plus the frontier sync
// that actually opens the lane for work - a lane starts waiting, and
// CompleteLaneWork's whole point is the runnable-to-terminal path.
func newRunnableTestLane(t *testing.T) (*Store, string, string) {
	t.Helper()
	s, orchID, laneID := newVerificationTestLane(t)
	if _, err := s.SyncFrontier(context.Background(), "frontier-1", orchID); err != nil {
		t.Fatalf("SyncFrontier: %v", err)
	}
	return s, orchID, laneID
}

func allVerificationsPassed() []LaneVerification {
	out := make([]LaneVerification, 0, len(fixedVerificationDimensionNames))
	for _, name := range fixedVerificationDimensionNames {
		out = append(out, LaneVerification{
			Name:    protocol.VerificationDimensionName(name),
			Status:  protocol.VerificationDimensionStatusPassed,
			Summary: "checked",
		})
	}
	return out
}

// The delivery that prompted this work completed with its only lane still
// runnable and all six dimensions pending, because nothing in the public
// tool surface could move a lane at all.
func TestCompleteLaneWorkTakesARunnableLaneToAccepted(t *testing.T) {
	s, orchID, laneID := newRunnableTestLane(t)
	ctx := context.Background()

	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	if lane.Status != protocol.DeliveryLaneStatusRunnable {
		t.Fatalf("lane status = %q, want runnable to start", lane.Status)
	}

	done, err := s.CompleteLaneWork(ctx, LaneCompletionRequest{
		OrchestrationID: orchID, LaneID: laneID, ExpectedRevision: lane.Revision,
		WorkerID: "semar", SessionID: "sess-1",
		Verifications:              allVerificationsPassed(),
		Outcome:                    protocol.DeliveryLaneStatusAccepted,
		Summary:                    "bumped the reactor version across every module",
		IndependenceOverrideReason: "single-agent delivery: no separate reviewer",
	})
	if err != nil {
		t.Fatalf("CompleteLaneWork: %v", err)
	}
	if done.Status != protocol.DeliveryLaneStatusAccepted {
		t.Fatalf("lane status = %q, want accepted", done.Status)
	}

	matrix, err := s.BuildVerificationMatrix(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("BuildVerificationMatrix: %v", err)
	}
	for _, d := range matrix.Dimensions {
		if d.Status == protocol.VerificationMatrixDimensionsElemStatusPending {
			t.Errorf("dimension %s is still pending after the lane was accepted", d.Name)
		}
	}

	conclusion, err := s.GetLatestReviewConclusion(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLatestReviewConclusion: %v", err)
	}
	if conclusion == nil || conclusion.Outcome != protocol.ReviewConclusionOutcomeApproved {
		t.Fatalf("review conclusion = %+v, want an approved one recorded", conclusion)
	}
}

func TestCompleteLaneWorkCanFailALane(t *testing.T) {
	s, orchID, laneID := newRunnableTestLane(t)
	ctx := context.Background()
	lane, _ := s.GetLane(ctx, orchID, laneID)

	done, err := s.CompleteLaneWork(ctx, LaneCompletionRequest{
		OrchestrationID: orchID, LaneID: laneID, ExpectedRevision: lane.Revision,
		WorkerID: "semar", SessionID: "sess-1",
		Verifications: []LaneVerification{{
			Name: protocol.VerificationDimensionNameUnit, Status: protocol.VerificationDimensionStatusFailed, Summary: "suite red",
		}},
		Outcome:                    protocol.DeliveryLaneStatusFailed,
		Summary:                    "could not make the suite pass",
		IndependenceOverrideReason: "single-agent delivery",
	})
	if err != nil {
		t.Fatalf("CompleteLaneWork: %v", err)
	}
	if done.Status != protocol.DeliveryLaneStatusFailed {
		t.Fatalf("lane status = %q, want failed", done.Status)
	}
	conclusion, err := s.GetLatestReviewConclusion(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLatestReviewConclusion: %v", err)
	}
	if conclusion == nil || conclusion.Outcome != protocol.ReviewConclusionOutcomeBlocked {
		t.Fatalf("review conclusion = %+v, want blocked", conclusion)
	}
}

// Without a stated override, the same session that did the work cannot
// also conclude the review - so a single-agent delivery has to say so out
// loud rather than closing the lane silently.
func TestCompleteLaneWorkRequiresAnOverrideWhenTheImplementerReviewsItself(t *testing.T) {
	s, orchID, laneID := newRunnableTestLane(t)
	ctx := context.Background()
	lane, _ := s.GetLane(ctx, orchID, laneID)

	_, err := s.CompleteLaneWork(ctx, LaneCompletionRequest{
		OrchestrationID: orchID, LaneID: laneID, ExpectedRevision: lane.Revision,
		WorkerID: "semar", SessionID: "sess-1",
		Verifications: allVerificationsPassed(),
		Outcome:       protocol.DeliveryLaneStatusAccepted,
		Summary:       "done",
	})
	if !errors.Is(err, ErrIndependenceRequired) {
		t.Fatalf("CompleteLaneWork error = %v, want ErrIndependenceRequired", err)
	}
}

func TestCompleteLaneWorkRejectsAStaleRevisionAndATerminalLane(t *testing.T) {
	s, orchID, laneID := newRunnableTestLane(t)
	ctx := context.Background()
	lane, _ := s.GetLane(ctx, orchID, laneID)

	req := LaneCompletionRequest{
		OrchestrationID: orchID, LaneID: laneID, ExpectedRevision: lane.Revision - 1,
		WorkerID: "semar", SessionID: "sess-1",
		Outcome: protocol.DeliveryLaneStatusAccepted, Summary: "done",
		IndependenceOverrideReason: "single-agent delivery",
	}
	if _, err := s.CompleteLaneWork(ctx, req); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision error = %v, want ErrRevisionConflict", err)
	}

	req.ExpectedRevision = lane.Revision
	done, err := s.CompleteLaneWork(ctx, req)
	if err != nil {
		t.Fatalf("CompleteLaneWork: %v", err)
	}
	req.ExpectedRevision = done.Revision
	req.IdempotencyKey = "second-attempt"
	if _, err := s.CompleteLaneWork(ctx, req); !errors.Is(err, ErrLaneTerminal) {
		t.Fatalf("second completion error = %v, want ErrLaneTerminal", err)
	}
}

func TestCompleteLaneWorkValidatesItsOwnRequest(t *testing.T) {
	s, orchID, laneID := newRunnableTestLane(t)
	ctx := context.Background()
	lane, _ := s.GetLane(ctx, orchID, laneID)
	base := LaneCompletionRequest{
		OrchestrationID: orchID, LaneID: laneID, ExpectedRevision: lane.Revision,
		WorkerID: "semar", SessionID: "sess-1",
		Outcome: protocol.DeliveryLaneStatusAccepted, Summary: "done",
	}

	cases := map[string]func(*LaneCompletionRequest){
		"no summary": func(r *LaneCompletionRequest) { r.Summary = "" },
		"no session": func(r *LaneCompletionRequest) { r.SessionID = "" },
		"non-terminal outcome": func(r *LaneCompletionRequest) {
			r.Outcome = protocol.DeliveryLaneStatusReview
		},
		"unknown dimension": func(r *LaneCompletionRequest) {
			r.Verifications = []LaneVerification{{Name: "vibes", Status: protocol.VerificationDimensionStatusPassed}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := base
			mutate(&req)
			if _, err := s.CompleteLaneWork(ctx, req); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}
