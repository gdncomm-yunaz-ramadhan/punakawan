package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// leasedLaneForRoleStages seeds an orchestration/project/task/lane,
// syncs it runnable, and grants a lease, returning the store and the
// leased lane ready for RecordRoleStage calls.
func leasedLaneForRoleStages(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)
	proj := registerProject(t, s, "rolestage-project")
	task := createTestTask(t, s, orch.Id, "rolestage task")
	if _, err := s.RouteParentTask(ctx, "route-1", orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	lane, err := s.CreateLane(ctx, "lane-1", NewID(), orch.Id, proj.Id, task.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	if _, err := s.SyncFrontier(ctx, "sync-1", orch.Id); err != nil {
		t.Fatalf("SyncFrontier: %v", err)
	}
	lane, err = s.GetLane(ctx, orch.Id, lane.Id)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	leased, err := s.GrantLease(ctx, "lease-1", orch.Id, lane.Id, lane.Revision, "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("GrantLease: %v", err)
	}
	return s, orch.Id, lane.Id, *leased.LeaseToken
}

func TestRecordRoleStageEnforcesOrder(t *testing.T) {
	s, orchID, laneID, token := leasedLaneForRoleStages(t)
	ctx := context.Background()

	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}

	if _, err := s.RecordRoleStage(ctx, "gareng-early", orchID, laneID, token, RoleStageGareng, "rec-gareng", lane.Revision); !errors.Is(err, ErrRoleStageOutOfOrder) {
		t.Fatalf("expected ErrRoleStageOutOfOrder recording gareng before semar, got %v", err)
	}

	afterSemar, err := s.RecordRoleStage(ctx, "semar-1", orchID, laneID, token, RoleStageSemar, "rec-semar", lane.Revision)
	if err != nil {
		t.Fatalf("RecordRoleStage(semar): %v", err)
	}
	if afterSemar.SemarRecordId == nil || *afterSemar.SemarRecordId != "rec-semar" {
		t.Fatalf("expected semar_record_id = rec-semar, got %+v", afterSemar.SemarRecordId)
	}

	if _, err := s.RecordRoleStage(ctx, "petruk-early", orchID, laneID, token, RoleStagePetruk, "rec-petruk", afterSemar.Revision); !errors.Is(err, ErrRoleStageOutOfOrder) {
		t.Fatalf("expected ErrRoleStageOutOfOrder recording petruk before gareng, got %v", err)
	}

	afterGareng, err := s.RecordRoleStage(ctx, "gareng-1", orchID, laneID, token, RoleStageGareng, "rec-gareng", afterSemar.Revision)
	if err != nil {
		t.Fatalf("RecordRoleStage(gareng): %v", err)
	}
	afterPetruk, err := s.RecordRoleStage(ctx, "petruk-1", orchID, laneID, token, RoleStagePetruk, "rec-petruk", afterGareng.Revision)
	if err != nil {
		t.Fatalf("RecordRoleStage(petruk): %v", err)
	}
	afterBagong, err := s.RecordRoleStage(ctx, "bagong-1", orchID, laneID, token, RoleStageBagong, "rec-bagong", afterPetruk.Revision)
	if err != nil {
		t.Fatalf("RecordRoleStage(bagong): %v", err)
	}
	if afterBagong.BagongRecordId == nil || *afterBagong.BagongRecordId != "rec-bagong" {
		t.Fatalf("expected bagong_record_id = rec-bagong, got %+v", afterBagong.BagongRecordId)
	}
}

func TestRecordRoleStageClearsLaterStagesOnResubmission(t *testing.T) {
	s, orchID, laneID, token := leasedLaneForRoleStages(t)
	ctx := context.Background()
	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}

	lane, err = s.RecordRoleStage(ctx, "semar-1", orchID, laneID, token, RoleStageSemar, "rec-semar", lane.Revision)
	if err != nil {
		t.Fatalf("RecordRoleStage(semar): %v", err)
	}
	lane, err = s.RecordRoleStage(ctx, "gareng-1", orchID, laneID, token, RoleStageGareng, "rec-gareng", lane.Revision)
	if err != nil {
		t.Fatalf("RecordRoleStage(gareng): %v", err)
	}
	lane, err = s.RecordRoleStage(ctx, "petruk-1", orchID, laneID, token, RoleStagePetruk, "rec-petruk", lane.Revision)
	if err != nil {
		t.Fatalf("RecordRoleStage(petruk): %v", err)
	}
	lane, err = s.RecordRoleStage(ctx, "bagong-1", orchID, laneID, token, RoleStageBagong, "rec-bagong", lane.Revision)
	if err != nil {
		t.Fatalf("RecordRoleStage(bagong): %v", err)
	}

	// Resubmitting gareng (e.g. after resolving a blocking question) must
	// invalidate petruk and bagong, since both were built against the
	// gareng review that just changed.
	afterResubmit, err := s.RecordRoleStage(ctx, "gareng-2", orchID, laneID, token, RoleStageGareng, "rec-gareng-v2", lane.Revision)
	if err != nil {
		t.Fatalf("RecordRoleStage(gareng resubmit): %v", err)
	}
	if afterResubmit.GarengRecordId == nil || *afterResubmit.GarengRecordId != "rec-gareng-v2" {
		t.Fatalf("expected updated gareng_record_id, got %+v", afterResubmit.GarengRecordId)
	}
	if afterResubmit.PetrukRecordId != nil {
		t.Fatalf("expected petruk_record_id cleared after gareng resubmission, got %v", *afterResubmit.PetrukRecordId)
	}
	if afterResubmit.BagongRecordId != nil {
		t.Fatalf("expected bagong_record_id cleared after gareng resubmission, got %v", *afterResubmit.BagongRecordId)
	}

	// Petruk cannot be recorded again until gareng's new output is
	// followed back through in order.
	if _, err := s.RecordRoleStage(ctx, "bagong-early", orchID, laneID, token, RoleStageBagong, "rec-bagong-v2", afterResubmit.Revision); !errors.Is(err, ErrRoleStageOutOfOrder) {
		t.Fatalf("expected ErrRoleStageOutOfOrder recording bagong before petruk is redone, got %v", err)
	}
}

func TestRecordRoleStageChecksLeaseToken(t *testing.T) {
	s, orchID, laneID, _ := leasedLaneForRoleStages(t)
	ctx := context.Background()
	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}

	if _, err := s.RecordRoleStage(ctx, "semar-wrong-token", orchID, laneID, "wrong-token", RoleStageSemar, "rec-semar", lane.Revision); !errors.Is(err, ErrLeaseTokenMismatch) {
		t.Fatalf("expected ErrLeaseTokenMismatch, got %v", err)
	}
}

func TestCompleteLeaseRequiresBagongStage(t *testing.T) {
	s, orchID, laneID, token := leasedLaneForRoleStages(t)
	ctx := context.Background()
	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}

	// A lane that never opts into the role-stage flow completes exactly
	// as before - no gate applies.
	unstaged, err := s.CompleteLease(ctx, "complete-unstaged", orchID, laneID, token, lane.Revision)
	if err != nil {
		t.Fatalf("CompleteLease with no role stages ever engaged: %v", err)
	}
	if unstaged.Status != protocol.DeliveryLaneStatusReview {
		t.Fatalf("expected review status, got %s", unstaged.Status)
	}

	// A second lease that DOES record Semar has opted in, and must reach
	// Bagong before it can complete.
	s2, orchID2, laneID2, token2 := leasedLaneForRoleStages(t)
	lane2, err := s2.GetLane(ctx, orchID2, laneID2)
	if err != nil {
		t.Fatalf("GetLane (second lane): %v", err)
	}
	lane2, err = s2.RecordRoleStage(ctx, "semar-2", orchID2, laneID2, token2, RoleStageSemar, "rec-semar", lane2.Revision)
	if err != nil {
		t.Fatalf("RecordRoleStage(semar): %v", err)
	}
	if _, err := s2.CompleteLease(ctx, "complete-early", orchID2, laneID2, token2, lane2.Revision); !errors.Is(err, ErrRoleStagesIncomplete) {
		t.Fatalf("expected ErrRoleStagesIncomplete once semar is recorded but bagong is not, got %v", err)
	}

	// RejectLease, unlike CompleteLease, never requires any role stage
	// even once a lane has opted in.
	rejected, err := s2.RejectLease(ctx, "reject-1", orchID2, laneID2, token2, lane2.Revision)
	if err != nil {
		t.Fatalf("RejectLease with semar recorded but bagong incomplete: %v", err)
	}
	if rejected.Status != protocol.DeliveryLaneStatusRunnable {
		t.Fatalf("expected the lane back to runnable after rejection, got %s", rejected.Status)
	}
}
