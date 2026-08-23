package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// newVerificationTestLane seeds an orchestration/project/routed task/lane
// and returns it ready for verification-dimension/CI-check/review-conclusion
// calls. Unlike leasedLaneForRoleStages, no lease is granted - none of
// this file's Store methods require one.
func newVerificationTestLane(t *testing.T) (*Store, string, string) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "verification-project")
	task := createTestTask(t, s, orch.Id, "verification task")
	if _, err := s.RouteParentTask(ctx, "route-1", orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	lane, err := s.CreateLane(ctx, "lane-1", NewID(), orch.Id, proj.Id, task.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	return s, orch.Id, lane.Id
}

func dimensionByName(matrix *protocol.VerificationMatrix, name protocol.VerificationMatrixDimensionsElemName) (protocol.VerificationMatrixDimensionsElem, bool) {
	for _, d := range matrix.Dimensions {
		if d.Name == name {
			return d, true
		}
	}
	return protocol.VerificationMatrixDimensionsElem{}, false
}

func TestBuildVerificationMatrixAlwaysHasSixPendingDimensionsInitially(t *testing.T) {
	s, orchID, laneID := newVerificationTestLane(t)
	ctx := context.Background()

	matrix, err := s.BuildVerificationMatrix(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("BuildVerificationMatrix: %v", err)
	}
	if len(matrix.Dimensions) != 6 {
		t.Fatalf("expected exactly 6 dimensions, got %d: %+v", len(matrix.Dimensions), matrix.Dimensions)
	}
	for _, d := range matrix.Dimensions {
		if d.Status != protocol.VerificationMatrixDimensionsElemStatusPending {
			t.Fatalf("expected dimension %s to default to pending, got %+v", d.Name, d)
		}
		if d.EvidenceId != nil || d.Summary != nil {
			t.Fatalf("expected no evidence_id/summary on an unrecorded dimension, got %+v", d)
		}
	}
	if matrix.ComputedAt.IsZero() {
		t.Fatal("expected computed_at to be set even with no lane events")
	}
}

func TestRecordVerificationDimensionLatestWins(t *testing.T) {
	s, orchID, laneID := newVerificationTestLane(t)
	ctx := context.Background()
	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}

	if err := s.RecordVerificationDimension(ctx, "vd-1", orchID, laneID, protocol.VerificationDimensionNameUnit, protocol.VerificationDimensionStatusFailed, "", "first attempt failed", lane.Revision); err != nil {
		t.Fatalf("RecordVerificationDimension (first): %v", err)
	}
	lane, err = s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	if err := s.RecordVerificationDimension(ctx, "vd-2", orchID, laneID, protocol.VerificationDimensionNameUnit, protocol.VerificationDimensionStatusPassed, "evidence-1", "retried and passed", lane.Revision); err != nil {
		t.Fatalf("RecordVerificationDimension (second): %v", err)
	}

	matrix, err := s.BuildVerificationMatrix(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("BuildVerificationMatrix: %v", err)
	}
	unit, ok := dimensionByName(matrix, protocol.VerificationMatrixDimensionsElemNameUnit)
	if !ok {
		t.Fatal("expected a unit dimension")
	}
	if unit.Status != protocol.VerificationMatrixDimensionsElemStatusPassed {
		t.Fatalf("expected latest-recorded status (passed) to win, got %+v", unit)
	}
	if unit.EvidenceId == nil || *unit.EvidenceId != "evidence-1" {
		t.Fatalf("expected evidence_id from the latest recording, got %+v", unit.EvidenceId)
	}
	if unit.Summary == nil || *unit.Summary != "retried and passed" {
		t.Fatalf("expected summary from the latest recording, got %+v", unit.Summary)
	}

	// Every other dimension is untouched and still defaults to pending.
	logic, ok := dimensionByName(matrix, protocol.VerificationMatrixDimensionsElemNameLogic)
	if !ok || logic.Status != protocol.VerificationMatrixDimensionsElemStatusPending {
		t.Fatalf("expected logic dimension to remain pending, got %+v", logic)
	}
}

func TestBuildVerificationMatrixDerivesCIFromRequiredChecks(t *testing.T) {
	s, orchID, laneID := newVerificationTestLane(t)
	ctx := context.Background()

	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	// A required check that later moves from running to passed.
	if err := s.RecordCICheck(ctx, "ci-1", orchID, laneID, protocol.CICheck{
		Provider: protocol.CICheckProviderGithub, ExternalId: "check-build", Name: "build",
		Status: protocol.CICheckStatusRunning, Required: true, ReportedAt: time.Now().UTC(),
	}, lane.Revision); err != nil {
		t.Fatalf("RecordCICheck (running): %v", err)
	}
	lane, err = s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	// An optional check that fails - must not affect the derived ci status.
	if err := s.RecordCICheck(ctx, "ci-2", orchID, laneID, protocol.CICheck{
		Provider: protocol.CICheckProviderGithub, ExternalId: "check-lint", Name: "lint",
		Status: protocol.CICheckStatusFailed, Required: false, ReportedAt: time.Now().UTC(),
	}, lane.Revision); err != nil {
		t.Fatalf("RecordCICheck (optional failed): %v", err)
	}

	matrix, err := s.BuildVerificationMatrix(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("BuildVerificationMatrix: %v", err)
	}
	ci, ok := dimensionByName(matrix, protocol.VerificationMatrixDimensionsElemNameCi)
	if !ok || ci.Status != protocol.VerificationMatrixDimensionsElemStatusPending {
		t.Fatalf("expected ci pending while the required check is still running, got %+v", ci)
	}

	// The required check now passes: latest status per external_id wins,
	// so the ci dimension should turn passed even though it was reported
	// running earlier - the optional check's failure never counts.
	lane, err = s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	if err := s.RecordCICheck(ctx, "ci-3", orchID, laneID, protocol.CICheck{
		Provider: protocol.CICheckProviderGithub, ExternalId: "check-build", Name: "build",
		Status: protocol.CICheckStatusPassed, Required: true, ReportedAt: time.Now().UTC(),
	}, lane.Revision); err != nil {
		t.Fatalf("RecordCICheck (passed): %v", err)
	}
	matrix, err = s.BuildVerificationMatrix(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("BuildVerificationMatrix: %v", err)
	}
	ci, ok = dimensionByName(matrix, protocol.VerificationMatrixDimensionsElemNameCi)
	if !ok || ci.Status != protocol.VerificationMatrixDimensionsElemStatusPassed {
		t.Fatalf("expected ci passed once the only required check's latest status is passed, got %+v", ci)
	}

	// A required check failing fails the whole ci dimension.
	lane, err = s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	if err := s.RecordCICheck(ctx, "ci-4", orchID, laneID, protocol.CICheck{
		Provider: protocol.CICheckProviderGithub, ExternalId: "check-e2e", Name: "e2e",
		Status: protocol.CICheckStatusFailed, Required: true, ReportedAt: time.Now().UTC(),
	}, lane.Revision); err != nil {
		t.Fatalf("RecordCICheck (second required failed): %v", err)
	}
	matrix, err = s.BuildVerificationMatrix(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("BuildVerificationMatrix: %v", err)
	}
	ci, ok = dimensionByName(matrix, protocol.VerificationMatrixDimensionsElemNameCi)
	if !ok || ci.Status != protocol.VerificationMatrixDimensionsElemStatusFailed {
		t.Fatalf("expected ci failed once any required check's latest status is failed, got %+v", ci)
	}
}

func TestBuildVerificationMatrixExplicitCIOverridesDerived(t *testing.T) {
	s, orchID, laneID := newVerificationTestLane(t)
	ctx := context.Background()

	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	if err := s.RecordCICheck(ctx, "ci-1", orchID, laneID, protocol.CICheck{
		Provider: protocol.CICheckProviderGithub, ExternalId: "check-build", Name: "build",
		Status: protocol.CICheckStatusFailed, Required: true, ReportedAt: time.Now().UTC(),
	}, lane.Revision); err != nil {
		t.Fatalf("RecordCICheck: %v", err)
	}

	lane, err = s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	// An explicit ci recording must win over the derived (failed) status.
	if err := s.RecordVerificationDimension(ctx, "vd-1", orchID, laneID, protocol.VerificationDimensionNameCi, protocol.VerificationDimensionStatusPassed, "evidence-override", "manually verified", lane.Revision); err != nil {
		t.Fatalf("RecordVerificationDimension: %v", err)
	}

	matrix, err := s.BuildVerificationMatrix(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("BuildVerificationMatrix: %v", err)
	}
	ci, ok := dimensionByName(matrix, protocol.VerificationMatrixDimensionsElemNameCi)
	if !ok || ci.Status != protocol.VerificationMatrixDimensionsElemStatusPassed {
		t.Fatalf("expected explicit ci recording to override the derived status, got %+v", ci)
	}
	if ci.EvidenceId == nil || *ci.EvidenceId != "evidence-override" {
		t.Fatalf("expected the explicit recording's evidence_id, got %+v", ci.EvidenceId)
	}
}

func TestRecordVerificationDimensionRejectsTerminalLane(t *testing.T) {
	s, orchID, laneID := newVerificationTestLane(t)
	ctx := context.Background()
	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	lane, err = s.UpdateLaneStatus(ctx, "fail-1", orchID, laneID, lane.Revision, protocol.DeliveryLaneStatusFailed)
	if err != nil {
		t.Fatalf("UpdateLaneStatus: %v", err)
	}

	if err := s.RecordVerificationDimension(ctx, "vd-1", orchID, laneID, protocol.VerificationDimensionNameUnit, protocol.VerificationDimensionStatusPassed, "", "", lane.Revision); !errors.Is(err, ErrLaneTerminal) {
		t.Fatalf("expected ErrLaneTerminal, got %v", err)
	}
}

func TestRecordCICheckRejectsTerminalLane(t *testing.T) {
	s, orchID, laneID := newVerificationTestLane(t)
	ctx := context.Background()
	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	lane, err = s.UpdateLaneStatus(ctx, "accept-1", orchID, laneID, lane.Revision, protocol.DeliveryLaneStatusAccepted)
	if err != nil {
		t.Fatalf("UpdateLaneStatus: %v", err)
	}

	check := protocol.CICheck{
		Provider: protocol.CICheckProviderGithub, ExternalId: "check-build", Name: "build",
		Status: protocol.CICheckStatusPassed, Required: true, ReportedAt: time.Now().UTC(),
	}
	if err := s.RecordCICheck(ctx, "ci-1", orchID, laneID, check, lane.Revision); !errors.Is(err, ErrLaneTerminal) {
		t.Fatalf("expected ErrLaneTerminal, got %v", err)
	}
}

func baseReviewConclusion() protocol.ReviewConclusion {
	return protocol.ReviewConclusion{
		Outcome:                      protocol.ReviewConclusionOutcomeApproved,
		ReviewerWorkerId:             "worker-1",
		EvidenceIds:                  []string{"evidence-1"},
		BlockingFindingIds:           []string{},
		VerificationMatrixComputedAt: time.Now().UTC(),
	}
}

func TestRecordReviewConclusionRequiresIndependenceOrOverride(t *testing.T) {
	s, orchID, laneID := newVerificationTestLane(t)
	ctx := context.Background()
	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}

	conclusion := baseReviewConclusion()
	conclusion.ReviewerSessionId = "session-implementer"
	conclusion.IndependenceLevel = protocol.ReviewConclusionIndependenceLevelSameSession

	if _, err := s.RecordReviewConclusion(ctx, "rc-1", orchID, laneID, conclusion, "session-implementer", lane.Revision); !errors.Is(err, ErrIndependenceRequired) {
		t.Fatalf("expected ErrIndependenceRequired for same-session review with no override, got %v", err)
	}
}

func TestRecordReviewConclusionHappyPathDifferentSession(t *testing.T) {
	s, orchID, laneID := newVerificationTestLane(t)
	ctx := context.Background()
	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}

	conclusion := baseReviewConclusion()
	conclusion.ReviewerSessionId = "session-reviewer"
	conclusion.IndependenceLevel = protocol.ReviewConclusionIndependenceLevelDifferentSession

	stored, err := s.RecordReviewConclusion(ctx, "rc-1", orchID, laneID, conclusion, "session-implementer", lane.Revision)
	if err != nil {
		t.Fatalf("RecordReviewConclusion: %v", err)
	}
	if stored.Id == "" {
		t.Fatal("expected a minted id")
	}
	if stored.LaneId != laneID {
		t.Fatalf("expected lane_id %s, got %s", laneID, stored.LaneId)
	}
	if stored.RecordedAt.IsZero() {
		t.Fatal("expected recorded_at to be set")
	}
	if stored.Outcome != protocol.ReviewConclusionOutcomeApproved {
		t.Fatalf("expected outcome to round-trip, got %s", stored.Outcome)
	}

	fetched, err := s.GetLatestReviewConclusion(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLatestReviewConclusion: %v", err)
	}
	if fetched.Id != stored.Id {
		t.Fatalf("expected GetLatestReviewConclusion to return the just-recorded conclusion, got %+v", fetched)
	}
}

func TestRecordReviewConclusionHappyPathSameSessionWithOverride(t *testing.T) {
	s, orchID, laneID := newVerificationTestLane(t)
	ctx := context.Background()
	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}

	reason := "single-operator spike; independent review deferred and explicitly acknowledged"
	conclusion := baseReviewConclusion()
	conclusion.ReviewerSessionId = "session-implementer"
	conclusion.IndependenceLevel = protocol.ReviewConclusionIndependenceLevelSameSession
	conclusion.IndependenceOverrideReason = &reason

	stored, err := s.RecordReviewConclusion(ctx, "rc-1", orchID, laneID, conclusion, "session-implementer", lane.Revision)
	if err != nil {
		t.Fatalf("RecordReviewConclusion with override: %v", err)
	}
	if stored.IndependenceOverrideReason == nil || *stored.IndependenceOverrideReason != reason {
		t.Fatalf("expected override reason to round-trip, got %+v", stored.IndependenceOverrideReason)
	}
}

func TestRecordReviewConclusionRejectsTerminalLane(t *testing.T) {
	s, orchID, laneID := newVerificationTestLane(t)
	ctx := context.Background()
	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	lane, err = s.UpdateLaneStatus(ctx, "fail-1", orchID, laneID, lane.Revision, protocol.DeliveryLaneStatusFailed)
	if err != nil {
		t.Fatalf("UpdateLaneStatus: %v", err)
	}

	conclusion := baseReviewConclusion()
	conclusion.ReviewerSessionId = "session-reviewer"
	conclusion.IndependenceLevel = protocol.ReviewConclusionIndependenceLevelDifferentSession

	if _, err := s.RecordReviewConclusion(ctx, "rc-1", orchID, laneID, conclusion, "session-implementer", lane.Revision); !errors.Is(err, ErrLaneTerminal) {
		t.Fatalf("expected ErrLaneTerminal, got %v", err)
	}
}

func TestGetLatestReviewConclusionNotFound(t *testing.T) {
	s, orchID, laneID := newVerificationTestLane(t)
	ctx := context.Background()

	if _, err := s.GetLatestReviewConclusion(ctx, orchID, laneID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound before any conclusion is recorded, got %v", err)
	}
}
