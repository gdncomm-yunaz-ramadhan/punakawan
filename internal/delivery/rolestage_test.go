package delivery

import (
	"context"
	"errors"
	"fmt"
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
	return leasedLaneForRoleStagesWithStore(t, s, "")
}

// fakeWorkflowDefinitionResolver is a minimal, in-memory
// WorkflowDefinitionResolver stand-in for internal/workflowdef.Store,
// so this package's own tests can exercise the definition-aware gate
// without depending on that package at all - the same decoupling the
// interface itself exists for.
type fakeWorkflowDefinitionResolver struct {
	enabled        map[string]bool
	requiredStages map[string]map[string]bool
}

func (f *fakeWorkflowDefinitionResolver) ValidateEnabled(ctx context.Context, id string) error {
	if !f.enabled[id] {
		return fmt.Errorf("fake workflow definition %q does not exist or is disabled", id)
	}
	return nil
}

func (f *fakeWorkflowDefinitionResolver) RequiredRoleStages(ctx context.Context, id string) (map[string]bool, error) {
	return f.requiredStages[id], nil
}

// leasedLaneForRoleStagesWithStore is leasedLaneForRoleStages but reuses
// an already-constructed store (so the caller can wire it with a
// WorkflowDefinitionResolver first) and optionally attaches
// workflowDefinitionID to the seeded orchestration.
func leasedLaneForRoleStagesWithStore(t *testing.T, s *Store, workflowDefinitionID string) (*Store, string, string, string) {
	t.Helper()
	ctx := context.Background()
	orch, err := s.CreateOrchestrationWithOptions(ctx, "orch-"+NewID(), NewID(), nil, OrchestrationOptions{WorkflowDefinitionID: workflowDefinitionID})
	if err != nil {
		t.Fatalf("CreateOrchestrationWithOptions: %v", err)
	}
	proj := registerProject(t, s, "rolestage-project-"+NewID())
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

// TestCompleteLeaseHonorsDefinitionOptionalStages: a definition that
// explicitly marks gareng and petruk not required lets CompleteLease
// succeed once only Semar and Bagong are recorded - both the ordering
// gate (RecordRoleStage) and the completion gate (CompleteLease) must
// skip the two optional stages.
func TestCompleteLeaseHonorsDefinitionOptionalStages(t *testing.T) {
	const defID = "hotfix-workflow"
	resolver := &fakeWorkflowDefinitionResolver{
		enabled: map[string]bool{defID: true},
		requiredStages: map[string]map[string]bool{
			defID: {"gareng": false, "petruk": false},
		},
	}
	s := NewStore(newTestDB(t), WithWorkflowDefinitionResolver(resolver))
	_, orchID, laneID, token := leasedLaneForRoleStagesWithStore(t, s, defID)
	ctx := context.Background()

	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}

	// Gareng and petruk are skipped entirely - recording bagong directly
	// after semar must not be treated as out of order.
	lane, err = s.RecordRoleStage(ctx, "semar-1", orchID, laneID, token, RoleStageSemar, "rec-semar", lane.Revision)
	if err != nil {
		t.Fatalf("RecordRoleStage(semar): %v", err)
	}
	lane, err = s.RecordRoleStage(ctx, "bagong-1", orchID, laneID, token, RoleStageBagong, "rec-bagong", lane.Revision)
	if err != nil {
		t.Fatalf("RecordRoleStage(bagong) skipping gareng/petruk: %v", err)
	}

	completed, err := s.CompleteLease(ctx, "complete-1", orchID, laneID, token, lane.Revision)
	if err != nil {
		t.Fatalf("CompleteLease with only semar+bagong recorded and gareng/petruk marked not required: %v", err)
	}
	if completed.Status != protocol.DeliveryLaneStatusReview {
		t.Fatalf("expected review status, got %s", completed.Status)
	}
}

// TestCompleteLeaseRequiresAllFourWithEmptyRolesMap: a definition
// attached to the orchestration but with an empty Roles map must not
// loosen the gate at all - a restriction only takes effect once it is
// stated, so every one of the four stages absent from an empty map
// still defaults to required.
func TestCompleteLeaseRequiresAllFourWithEmptyRolesMap(t *testing.T) {
	const defID = "no-restrictions-workflow"
	resolver := &fakeWorkflowDefinitionResolver{
		enabled:        map[string]bool{defID: true},
		requiredStages: map[string]map[string]bool{defID: {}},
	}
	s := NewStore(newTestDB(t), WithWorkflowDefinitionResolver(resolver))
	_, orchID, laneID, token := leasedLaneForRoleStagesWithStore(t, s, defID)
	ctx := context.Background()

	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	lane, err = s.RecordRoleStage(ctx, "semar-1", orchID, laneID, token, RoleStageSemar, "rec-semar", lane.Revision)
	if err != nil {
		t.Fatalf("RecordRoleStage(semar): %v", err)
	}

	if _, err := s.RecordRoleStage(ctx, "bagong-early", orchID, laneID, token, RoleStageBagong, "rec-bagong", lane.Revision); !errors.Is(err, ErrRoleStageOutOfOrder) {
		t.Fatalf("expected ErrRoleStageOutOfOrder skipping gareng/petruk with an empty Roles map, got %v", err)
	}

	if _, err := s.CompleteLease(ctx, "complete-early", orchID, laneID, token, lane.Revision); !errors.Is(err, ErrRoleStagesIncomplete) {
		t.Fatalf("expected ErrRoleStagesIncomplete with an empty Roles map (all four still required), got %v", err)
	}
}

// TestCreateOrchestrationWithOptionsRejectsUnknownOrDisabled:
// attaching a workflow_definition_id that does not exist, or names a
// disabled definition, is rejected at attach time rather than silently
// ignored, and with no resolver configured at all it fails closed
// rather than silently accepting the id unchecked.
func TestCreateOrchestrationWithOptionsRejectsUnknownOrDisabled(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown id", func(t *testing.T) {
		resolver := &fakeWorkflowDefinitionResolver{enabled: map[string]bool{}}
		s := NewStore(newTestDB(t), WithWorkflowDefinitionResolver(resolver))
		if _, err := s.CreateOrchestrationWithOptions(ctx, "orch-unknown", NewID(), nil, OrchestrationOptions{WorkflowDefinitionID: "does-not-exist"}); err == nil {
			t.Fatal("expected an error attaching an unknown workflow_definition_id")
		}
	})

	t.Run("disabled id", func(t *testing.T) {
		resolver := &fakeWorkflowDefinitionResolver{enabled: map[string]bool{"disabled-workflow": false}}
		s := NewStore(newTestDB(t), WithWorkflowDefinitionResolver(resolver))
		if _, err := s.CreateOrchestrationWithOptions(ctx, "orch-disabled", NewID(), nil, OrchestrationOptions{WorkflowDefinitionID: "disabled-workflow"}); err == nil {
			t.Fatal("expected an error attaching a disabled workflow_definition_id")
		}
	})

	t.Run("no resolver configured", func(t *testing.T) {
		s := newTestStore(t)
		if _, err := s.CreateOrchestrationWithOptions(ctx, "orch-noresolver", NewID(), nil, OrchestrationOptions{WorkflowDefinitionID: "some-workflow"}); err == nil {
			t.Fatal("expected an error attaching a workflow_definition_id with no resolver configured")
		}
	})
}
