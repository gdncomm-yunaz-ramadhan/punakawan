package delivery

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func gapCodes(r Readiness) []string {
	out := make([]string, 0, len(r.Gaps))
	for _, g := range r.Gaps {
		out = append(out, g.Code)
	}
	return out
}

func hasGap(r Readiness, code string) bool {
	for _, g := range r.Gaps {
		if g.Code == code {
			return true
		}
	}
	return false
}

// This is the exact shape of the delivery that prompted the work: an open
// lane, entirely pending verification, and unpriced usage - completed
// without anything saying a word.
func TestAssessCompletionReadinessCatchesAnOpenLane(t *testing.T) {
	s, orchID, laneID := newRunnableTestLane(t)
	ctx := context.Background()

	view, err := s.BuildDeliveryView(ctx, orchID)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	readiness := AssessCompletionReadiness(view)
	if readiness.Ready {
		t.Fatalf("readiness = ready, want a lane_not_terminal gap")
	}
	if !hasGap(readiness, GapLaneNotTerminal) {
		t.Fatalf("gaps = %v, want %s", gapCodes(readiness), GapLaneNotTerminal)
	}
	// An open lane's dimensions are pending by definition; saying so
	// separately would be the same complaint twice.
	if hasGap(readiness, GapVerificationPending) {
		t.Errorf("gaps = %v, want no separate verification gap for a lane that never closed", gapCodes(readiness))
	}
	for _, g := range readiness.Gaps {
		if g.Code == GapLaneNotTerminal && (len(g.Subjects) != 1 || g.Subjects[0] != laneID) {
			t.Errorf("gap subjects = %v, want the open lane id %q", g.Subjects, laneID)
		}
	}
}

func TestAssessCompletionReadinessCatchesPartialVerificationOnAClosedLane(t *testing.T) {
	s, orchID, laneID := newRunnableTestLane(t)
	ctx := context.Background()
	lane, _ := s.GetLane(ctx, orchID, laneID)

	if _, err := s.CompleteLaneWork(ctx, LaneCompletionRequest{
		OrchestrationID: orchID, LaneID: laneID, ExpectedRevision: lane.Revision,
		WorkerID: "semar", SessionID: "sess-1",
		Verifications: []LaneVerification{{
			Name: protocol.VerificationDimensionNameUnit, Status: protocol.VerificationDimensionStatusPassed,
		}},
		Outcome: protocol.DeliveryLaneStatusAccepted, Summary: "done",
		IndependenceOverrideReason: "single-agent delivery",
	}); err != nil {
		t.Fatalf("CompleteLaneWork: %v", err)
	}

	view, err := s.BuildDeliveryView(ctx, orchID)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	readiness := AssessCompletionReadiness(view)
	if hasGap(readiness, GapLaneNotTerminal) {
		t.Errorf("gaps = %v, want no open-lane gap once the lane is accepted", gapCodes(readiness))
	}
	if !hasGap(readiness, GapVerificationPending) {
		t.Fatalf("gaps = %v, want %s - five dimensions were never reported on", gapCodes(readiness), GapVerificationPending)
	}
}

func TestAssessCompletionReadinessIsCleanWhenEverythingIsReported(t *testing.T) {
	s, orchID, laneID := newRunnableTestLane(t)
	ctx := context.Background()
	lane, _ := s.GetLane(ctx, orchID, laneID)

	if _, err := s.CompleteLaneWork(ctx, LaneCompletionRequest{
		OrchestrationID: orchID, LaneID: laneID, ExpectedRevision: lane.Revision,
		WorkerID: "semar", SessionID: "sess-1",
		Verifications: allVerificationsPassed(),
		Outcome:       protocol.DeliveryLaneStatusAccepted, Summary: "done",
		IndependenceOverrideReason: "single-agent delivery",
	}); err != nil {
		t.Fatalf("CompleteLaneWork: %v", err)
	}

	view, err := s.BuildDeliveryView(ctx, orchID)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if readiness := AssessCompletionReadiness(view); !readiness.Ready {
		t.Fatalf("readiness gaps = %v, want none", gapCodes(readiness))
	}
}

func TestAssessCompletionReadinessCatchesAnUnsyncedWorklog(t *testing.T) {
	view := &DeliveryView{
		WorkLogs: []WorkLogEntry{
			{ID: "wl-1", SyncStatus: "pending"},
			{ID: "wl-2", SyncStatus: "synced"},
			{ID: "wl-3", SyncStatus: "failed"},
		},
	}
	readiness := AssessCompletionReadiness(view)
	if !hasGap(readiness, GapWorkLogUnsynced) {
		t.Fatalf("gaps = %v, want %s", gapCodes(readiness), GapWorkLogUnsynced)
	}
	for _, g := range readiness.Gaps {
		if g.Code != GapWorkLogUnsynced {
			continue
		}
		if len(g.Subjects) != 2 || g.Subjects[0] != "wl-1" || g.Subjects[1] != "wl-3" {
			t.Errorf("gap subjects = %v, want the pending and failed worklogs only", g.Subjects)
		}
	}
}

func TestAssessCompletionReadinessNamesUncoveredRequirements(t *testing.T) {
	parent, subtask := "TRF-22576", "TRF-22577"
	view := &DeliveryView{
		RequirementSources: []*protocol.RequirementSource{
			{ExternalId: &parent},
			{ExternalId: &subtask},
		},
		Lifecycle: &DeliveryLifecycle{
			WorkItems: []JiraWorkItemMapping{{JiraIssueKey: parent}},
		},
	}
	readiness := AssessCompletionReadiness(view)
	if !hasGap(readiness, GapRequirementUncovered) {
		t.Fatalf("gaps = %v, want %s", gapCodes(readiness), GapRequirementUncovered)
	}
	for _, g := range readiness.Gaps {
		if g.Code == GapRequirementUncovered && (len(g.Subjects) != 1 || g.Subjects[0] != subtask) {
			t.Errorf("gap subjects = %v, want just the uncovered subtask %q", g.Subjects, subtask)
		}
	}
}

// A delivery that bound nothing at all has a different, louder problem
// (its lanes are open). Calling every requirement uncovered on top of
// that is noise, not information.
func TestAssessCompletionReadinessStaysQuietWhenNothingIsBoundYet(t *testing.T) {
	key := "TRF-1"
	view := &DeliveryView{RequirementSources: []*protocol.RequirementSource{{ExternalId: &key}}}
	if hasGap(AssessCompletionReadiness(view), GapRequirementUncovered) {
		t.Fatal("expected no uncovered-requirement gap when no work item is mapped at all")
	}
}

func TestAssessCompletionReadinessNamesTheUnpricedModel(t *testing.T) {
	view := &DeliveryView{}
	view.Telemetry.TelemetryStatus = "incomplete"
	view.Telemetry.UnpricedModels = []string{"claude-opus-5"}

	readiness := AssessCompletionReadiness(view)
	if !hasGap(readiness, GapCostUnknown) {
		t.Fatalf("gaps = %v, want %s", gapCodes(readiness), GapCostUnknown)
	}
	for _, g := range readiness.Gaps {
		if g.Code == GapCostUnknown && g.Detail != "no catalog price for claude-opus-5" {
			t.Errorf("gap detail = %q, want it to name the model", g.Detail)
		}
	}
}

func TestRecordWaivedGapsRecordsWithoutChangingStatus(t *testing.T) {
	s, orchID, _ := newRunnableTestLane(t)
	ctx := context.Background()

	before, err := s.BuildDeliveryView(ctx, orchID)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	readiness := AssessCompletionReadiness(before)
	if readiness.Ready {
		t.Fatal("expected gaps to waive")
	}
	if err := s.RecordWaivedGaps(ctx, NewID(), orchID, readiness.Gaps); err != nil {
		t.Fatalf("RecordWaivedGaps: %v", err)
	}

	after, err := s.BuildDeliveryView(ctx, orchID)
	if err != nil {
		t.Fatalf("BuildDeliveryView after waiver: %v", err)
	}
	if after.Orchestration.Status != before.Orchestration.Status {
		t.Fatalf("status = %q, want the waiver record to leave it at %q", after.Orchestration.Status, before.Orchestration.Status)
	}
	var found bool
	for _, ev := range after.Timeline {
		if ev.Type == protocol.DeliveryEventTypeOrchestrationCompletedWithGaps {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the waived gaps to appear in the delivery's own timeline")
	}
}

// The waiver always follows the orchestration.completed it annotates, so
// it must never reopen a completed delivery - which the reducer's
// "any later event promotes pending to active" rule would otherwise do.
func TestWaivedGapsEventLeavesACompletedOrchestrationCompleted(t *testing.T) {
	orchID := NewID()
	events := []protocol.DeliveryEvent{
		{OrchestrationId: orchID, Type: protocol.DeliveryEventTypeOrchestrationCreated, Payload: map[string]any{}},
		{OrchestrationId: orchID, Type: protocol.DeliveryEventTypeOrchestrationCompleted, Payload: map[string]any{}},
		{OrchestrationId: orchID, Type: protocol.DeliveryEventTypeOrchestrationCompletedWithGaps, Payload: map[string]any{}},
	}
	orch, err := reduceOrchestration(orchID, events)
	if err != nil {
		t.Fatalf("reduceOrchestration: %v", err)
	}
	if orch.Status != protocol.DeliveryOrchestrationStatusCompleted {
		t.Fatalf("status = %q, want completed", orch.Status)
	}
}
