package mcpserver

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/worklogalloc"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// seedRunnableLaneFreetext is seedRunnableLane's own fixture (see
// tools_delivery_test.go), except its one requirement source is
// freetext rather than jira-sourced. request_project_approval's
// ready/idempotency tests use this instead of seedRunnableLane so they
// never give gatherJiraSubtasksForProject a Jira-sourced task to look
// up - resolving a real atlassian Gate would start whatever real
// adapter process this machine happens to have configured globally
// (internal/workspace.LoadGlobalConfig is not sandboxed per test), which
// these tests have no need to depend on.
func seedRunnableLaneFreetext(t *testing.T, a *app.App) (orchestrationID, laneID string) {
	t.Helper()
	ctx := context.Background()
	db, err := a.OpenStorage(ctx)
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	store := delivery.NewStore(db)

	orch, err := store.CreateOrchestration(ctx, delivery.NewID(), delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	proj, err := store.RegisterProject(ctx, delivery.NewID(), delivery.NewID(), "project-approval-mcp-test", "https://example.test/project-approval-mcp-test.git", "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	source, err := store.CaptureRequirement(ctx, delivery.NewID(), orch.Id, delivery.SourceInput{Provider: "freetext", Title: "seed requirement"})
	if err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
	task, err := store.CreateParentTask(ctx, delivery.NewID(), delivery.NewID(), orch.Id, "seed task", []string{source.Id})
	if err != nil {
		t.Fatalf("CreateParentTask: %v", err)
	}
	if _, err := store.RouteParentTask(ctx, delivery.NewID(), orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	lane, err := store.CreateLane(ctx, delivery.NewID(), delivery.NewID(), orch.Id, proj.Id, task.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	return orch.Id, lane.Id
}

// setDeliveryProfileForTest gives projectID the minimal profile
// MergeReadiness/RunPreflight need: a base branch and one required
// verification gate (unit), mirroring
// TestVerificationAndMergeReadinessEndToEnd's own setup.
func setDeliveryProfileForTest(t *testing.T, store *delivery.Store, projectID string) {
	t.Helper()
	if _, err := store.SetDeliveryProfile(context.Background(), delivery.NewID(), delivery.NewID(), projectID, delivery.ProfileInput{
		BaseBranch:        "main",
		VerificationGates: []string{"unit"},
	}); err != nil {
		t.Fatalf("SetDeliveryProfile: %v", err)
	}
}

// makeLaneReady drives laneID through record_verification_dimension
// (unit, passed) and submit_review_conclusion (approved) over the real
// MCP wire, the same sequence TestVerificationAndMergeReadinessEndToEnd
// uses to prove out check_merge_readiness.
func makeLaneReady(t *testing.T, ctx context.Context, store *delivery.Store, orchID, laneID string) {
	t.Helper()
	lane, err := store.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	if err := store.RecordVerificationDimension(ctx, delivery.NewID(), orchID, laneID, "unit", "passed", "evidence-1", "unit tests passed", lane.Revision); err != nil {
		t.Fatalf("RecordVerificationDimension: %v", err)
	}
	lane, err = store.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane after verification: %v", err)
	}
	conclusion := protocol.ReviewConclusion{
		Id:                           "placeholder",
		LaneId:                       "placeholder",
		RecordedAt:                   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Outcome:                      protocol.ReviewConclusionOutcomeApproved,
		ReviewerWorkerId:             "worker-1",
		ReviewerSessionId:            "session-reviewer",
		IndependenceLevel:            protocol.ReviewConclusionIndependenceLevelDifferentSession,
		EvidenceIds:                  []string{"evidence-1"},
		BlockingFindingIds:           []string{},
		VerificationMatrixComputedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if _, err := store.RecordReviewConclusion(ctx, delivery.NewID(), orchID, laneID, conclusion, "session-implementer", lane.Revision); err != nil {
		t.Fatalf("RecordReviewConclusion: %v", err)
	}
}

// TestRequestProjectApprovalNotReadyReportsFailingGates proves a project
// whose only lane hasn't satisfied its verification gates or review yet
// gets an honest not-ready result naming the blocking gates, and creates
// no manifest at all.
func TestRequestProjectApprovalNotReadyReportsFailingGates(t *testing.T) {
	a := newTestApp(t)
	orchID, laneID := seedRunnableLane(t, a)
	cs := connect(t, a)

	ctx := context.Background()
	store, err := openDeliveryStore(ctx, a)
	if err != nil {
		t.Fatalf("openDeliveryStore: %v", err)
	}
	lane, err := store.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	setDeliveryProfileForTest(t, store, lane.ProjectId)

	var out RequestProjectApprovalOutput
	callTool(t, cs, "request_project_approval", map[string]any{
		"orchestration_id": orchID,
		"project_id":       lane.ProjectId,
	}, &out)

	if out.Ready {
		t.Fatalf("expected not ready before any verification or review, got %+v", out)
	}
	if out.Manifest != nil {
		t.Fatalf("expected no manifest created while not ready, got %+v", out.Manifest)
	}
	if len(out.NotReadyLanes) != 1 || out.NotReadyLanes[0].LaneId != laneID {
		t.Fatalf("expected the seeded lane reported not-ready, got %+v", out.NotReadyLanes)
	}
	if len(out.NotReadyLanes[0].FailingGates) == 0 {
		t.Fatalf("expected non-empty failing gates, got %+v", out.NotReadyLanes[0])
	}
}

// TestRequestProjectApprovalCreatesManifestWhenReady proves that once
// every lane in a project passes MergeReadiness, request_project_approval
// runs preflight and creates a real ApprovalManifest carrying those
// checks - and, since this project's one requirement is freetext-sourced
// (no Jira issue to look subtasks up for) and the lane has no run-scoped
// verified-time evidence, that the proposed worklog degrades to the
// honest zero-value allocation rather than erroring the whole call (the
// "no Jira subtasks configured" case).
func TestRequestProjectApprovalCreatesManifestWhenReady(t *testing.T) {
	a := newTestApp(t)
	orchID, laneID := seedRunnableLaneFreetext(t, a)
	cs := connect(t, a)

	ctx := context.Background()
	store, err := openDeliveryStore(ctx, a)
	if err != nil {
		t.Fatalf("openDeliveryStore: %v", err)
	}
	lane, err := store.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	setDeliveryProfileForTest(t, store, lane.ProjectId)
	makeLaneReady(t, ctx, store, orchID, laneID)

	var out RequestProjectApprovalOutput
	callTool(t, cs, "request_project_approval", map[string]any{
		"orchestration_id": orchID,
		"project_id":       lane.ProjectId,
	}, &out)

	if !out.Ready {
		t.Fatalf("expected ready once the lane's gates and review are satisfied, got %+v", out)
	}
	if len(out.NotReadyLanes) != 0 {
		t.Fatalf("expected no not-ready lanes once ready, got %+v", out.NotReadyLanes)
	}
	if out.Manifest == nil {
		t.Fatal("expected a manifest once ready")
	}
	if len(out.Manifest.Checks) == 0 {
		t.Fatal("expected preflight checks recorded on the manifest")
	}
	if out.Manifest.ProjectId != lane.ProjectId {
		t.Fatalf("manifest project_id = %q, want %q", out.Manifest.ProjectId, lane.ProjectId)
	}
	if len(out.Manifest.ParentTaskIds) != 1 {
		t.Fatalf("expected the manifest scoped to exactly the one routed parent task, got %+v", out.Manifest.ParentTaskIds)
	}
	if out.Manifest.ProposedWorklogTotalHours != nil {
		t.Fatalf("expected no proposed worklog total with no verified hours and no jira adapter configured, got %v", *out.Manifest.ProposedWorklogTotalHours)
	}
	if len(out.Manifest.ProposedWorklog) != 0 {
		t.Fatalf("expected no proposed worklog entries, got %+v", out.Manifest.ProposedWorklog)
	}
}

// TestRequestProjectApprovalIsIdempotentOnRepeatCall proves re-calling
// request_project_approval with the same orchestration/project, once the
// same set of parent tasks is still the ready scope, returns the
// already-created manifest instead of minting a second one.
func TestRequestProjectApprovalIsIdempotentOnRepeatCall(t *testing.T) {
	a := newTestApp(t)
	orchID, laneID := seedRunnableLaneFreetext(t, a)
	cs := connect(t, a)

	ctx := context.Background()
	store, err := openDeliveryStore(ctx, a)
	if err != nil {
		t.Fatalf("openDeliveryStore: %v", err)
	}
	lane, err := store.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	setDeliveryProfileForTest(t, store, lane.ProjectId)
	makeLaneReady(t, ctx, store, orchID, laneID)

	args := map[string]any{"orchestration_id": orchID, "project_id": lane.ProjectId}

	var first RequestProjectApprovalOutput
	callTool(t, cs, "request_project_approval", args, &first)
	if !first.Ready || first.Manifest == nil {
		t.Fatalf("expected the first call to create a manifest, got %+v", first)
	}

	var second RequestProjectApprovalOutput
	callTool(t, cs, "request_project_approval", args, &second)
	if !second.Ready || second.Manifest == nil {
		t.Fatalf("expected the second call to also report ready with a manifest, got %+v", second)
	}
	if second.Manifest.Id != first.Manifest.Id {
		t.Fatalf("expected the same manifest id on repeat call, got %q then %q", first.Manifest.Id, second.Manifest.Id)
	}
	if second.Manifest.Revision != first.Manifest.Revision {
		t.Fatalf("expected the same manifest revision (no second write), got %d then %d", first.Manifest.Revision, second.Manifest.Revision)
	}
}

// singleAdapterGateProvider implements adapterGateProvider, returning a
// fixed Gate for exactly one adapter id, mirroring
// tools_createpr_test.go's fakeGateProvider pattern.
type singleAdapterGateProvider struct {
	adapterID string
	gate      *adapters.Gate
}

func (f singleAdapterGateProvider) Gate(ctx context.Context, adapterID string) (*adapters.Gate, error) {
	if adapterID != f.adapterID || f.gate == nil {
		return nil, fmt.Errorf("no %s adapter configured", adapterID)
	}
	return f.gate, nil
}

const jiraIssueWithSubtasksJSON = `{"normalized":{"key":"PAY-1","summary":"Parent","status":"In Progress","subtasks":[
	{"key":"PAY-2","summary":"Development work","status":"To Do"},
	{"key":"PAY-3","summary":"Testing work","status":"To Do"}
]}}`

// TestGatherJiraSubtasksForProjectDedupsAcrossTasksAndFeedsAllocate proves
// the Jira-subtask-gathering path used when a project's parent tasks do
// have Jira-sourced requirements: it fetches each distinct issue's
// subtasks once, dedups by subtask key, and the result plugs directly
// into worklogalloc.Allocate to produce a real, non-zero proposed
// worklog split - the "happy path ... correct worklog allocation" case,
// exercised without needing to fabricate real run-scoped verified-time
// evidence.
func TestGatherJiraSubtasksForProjectDedupsAcrossTasksAndFeedsAllocate(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	db, err := a.OpenStorage(ctx)
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	store := delivery.NewStore(db)

	orch, err := store.CreateOrchestration(ctx, delivery.NewID(), delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	proj, err := store.RegisterProject(ctx, delivery.NewID(), delivery.NewID(), "jira-worklog-project", "https://example.test/jira-worklog-project.git", "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	source, err := store.CaptureRequirement(ctx, delivery.NewID(), orch.Id, delivery.SourceInput{Provider: "jira", ExternalID: "PAY-1", Title: "parent issue"})
	if err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
	task, err := store.CreateParentTask(ctx, delivery.NewID(), delivery.NewID(), orch.Id, "task", []string{source.Id})
	if err != nil {
		t.Fatalf("CreateParentTask: %v", err)
	}
	if _, err := store.RouteParentTask(ctx, delivery.NewID(), orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}

	approvalStore := newTestApprovalStore(t)
	fc := &fakeAtlassianCaller{responses: map[string]string{"atlassian.getJiraIssue": jiraIssueWithSubtasksJSON}}
	gate := adapters.NewGate("atlassian", atlassianTestManifest(), fc, approvalStore)
	registry := singleAdapterGateProvider{adapterID: "atlassian", gate: gate}

	subtasks, err := gatherJiraSubtasksForProject(ctx, nil, registry, "semar", store, orch.Id, []string{task.Id})
	if err != nil {
		t.Fatalf("gatherJiraSubtasksForProject: %v", err)
	}
	if len(subtasks) != 2 {
		t.Fatalf("expected 2 subtasks gathered, got %+v", subtasks)
	}

	alloc := worklogalloc.Allocate(6, subtasks)
	if alloc.TotalHours != 6 || alloc.UnmappedHours != 0 {
		t.Fatalf("expected the full 6 hours mapped across dev/test buckets, got %+v", alloc)
	}
	if len(alloc.Worklogs) != 2 {
		t.Fatalf("expected 2 matched worklog buckets, got %+v", alloc.Worklogs)
	}
	for _, w := range alloc.Worklogs {
		if w.Hours != 3 {
			t.Fatalf("expected an even 3h split per matched bucket, got %+v", w)
		}
	}
}

// TestGatherJiraSubtasksForProjectNoAdapterConfigured proves a project
// with no atlassian adapter configured at all degrades to an empty
// subtask list rather than an error - the "no Jira subtasks configured"
// case at the gathering function's own level.
func TestGatherJiraSubtasksForProjectNoAdapterConfigured(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	subtasks, err := gatherJiraSubtasksForProject(ctx, nil, a.AdapterRegistry, "semar", nil, "orch-does-not-matter", nil)
	if err != nil {
		t.Fatalf("expected no error when no atlassian adapter is configured, got %v", err)
	}
	if len(subtasks) != 0 {
		t.Fatalf("expected zero subtasks, got %+v", subtasks)
	}
}
