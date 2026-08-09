package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// claimSeededLane lists and claims seedRunnableLane's lane over the real
// MCP wire, returning the lease token and revision needed for further
// calls against it.
func claimSeededLane(t *testing.T, cs *mcp.ClientSession, orchID, laneID string) (leaseToken string, revision int) {
	t.Helper()
	var listOut ListRunnableLanesOutput
	callTool(t, cs, "list_runnable_lanes", map[string]any{"orchestration_id": orchID}, &listOut)
	if len(listOut.Lanes) != 1 {
		t.Fatalf("expected exactly one runnable lane, got %+v", listOut.Lanes)
	}
	var claimOut LaneOutput
	callTool(t, cs, "claim_lane", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"expected_revision": listOut.Lanes[0].Revision,
		"worker_id":         "agent-1",
	}, &claimOut)
	return *claimOut.Lane.LeaseToken, claimOut.Lane.Revision
}

// fixedPRProvider is a minimal delivery.PRProvider used only to seed a
// lane's pr_number directly through the Store, bypassing the MCP layer
// entirely - used to set up the idempotent-resume scenario without any
// github adapter involved.
type fixedPRProvider struct {
	number int
	url    string
}

func (f fixedPRProvider) Publish(ctx context.Context, req delivery.PublishPRRequest) (int, string, error) {
	return f.number, f.url, nil
}

// TestPublishPrIdempotentResumeOverMCPWire proves publish_pr's idempotent
// resume behavior end to end: once a lane already has a published pull
// request (seeded directly through the Store here, since no github
// adapter is configured for this test's minimal workspace), calling
// publish_pr again over the real MCP wire returns the same pr fields
// without ever needing an adapter connection at all.
func TestPublishPrIdempotentResumeOverMCPWire(t *testing.T) {
	a := newTestApp(t)
	orchID, laneID := seedRunnableLane(t, a)
	cs := connect(t, a)

	leaseToken, revision := claimSeededLane(t, cs, orchID, laneID)

	ctx := context.Background()
	store, err := openDeliveryStore(ctx, a)
	if err != nil {
		t.Fatalf("openDeliveryStore: %v", err)
	}
	seeded, err := store.PublishPullRequest(ctx, delivery.NewID(), orchID, laneID, leaseToken, fixedPRProvider{number: 99, url: "https://example.com/pr/99"}, delivery.PublishPRRequest{
		RepoSlug: "acme/widgets", BaseBranch: "main", HeadBranch: "punakawan/widgets",
	}, revision)
	if err != nil {
		t.Fatalf("seed PublishPullRequest: %v", err)
	}
	if seeded.PrNumber == nil || *seeded.PrNumber != 99 {
		t.Fatalf("expected seeded pr_number 99, got %+v", seeded.PrNumber)
	}

	// No github adapter is configured anywhere in this test's app - if
	// publish_pr's resume path needed one, this call would fail closed
	// instead of returning the already-published pr fields.
	var out LaneOutput
	callTool(t, cs, "publish_pr", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"lease_token":       leaseToken,
		"expected_revision": seeded.Revision,
		"repo_slug":         "acme/widgets",
		"base_branch":       "main",
		"head_branch":       "punakawan/widgets",
		"title":             "Widgets",
		"body":              "body",
	}, &out)
	if out.Lane.PrNumber == nil || *out.Lane.PrNumber != 99 {
		t.Fatalf("expected the already-published pr_number 99 to be returned unchanged, got %+v", out.Lane.PrNumber)
	}
	if out.Lane.PrUrl == nil || *out.Lane.PrUrl != "https://example.com/pr/99" {
		t.Fatalf("expected the already-published pr_url to be returned unchanged, got %+v", out.Lane.PrUrl)
	}
}

// TestPublishPrFailsClosedWithoutAdapterOrLease covers two of publish_pr's
// failure paths over the real MCP wire: a wrong lease token (rejected
// before any adapter is ever needed), and a genuine first-time publish
// attempt with a valid lease but no github adapter configured (rejected
// with a clear reason instead of silently doing nothing).
func TestPublishPrFailsClosedWithoutAdapterOrLease(t *testing.T) {
	a := newTestApp(t)
	orchID, laneID := seedRunnableLane(t, a)
	cs := connect(t, a)
	leaseToken, revision := claimSeededLane(t, cs, orchID, laneID)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "publish_pr", Arguments: map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"lease_token":       "wrong-token",
		"expected_revision": revision,
		"repo_slug":         "acme/widgets",
		"base_branch":       "main",
		"head_branch":       "punakawan/widgets",
		"title":             "Widgets",
		"body":              "body",
	}})
	if err != nil {
		t.Fatalf("CallTool(publish_pr wrong token): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected publish_pr with a wrong lease token to fail")
	}

	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "publish_pr", Arguments: map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"lease_token":       leaseToken,
		"expected_revision": revision,
		"repo_slug":         "acme/widgets",
		"base_branch":       "main",
		"head_branch":       "punakawan/widgets",
		"title":             "Widgets",
		"body":              "body",
	}})
	if err != nil {
		t.Fatalf("CallTool(publish_pr): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a genuine first-time publish to fail with no github adapter configured")
	}
}

// TestPublishPrHappyPathViaFakeGate exercises publishPr's actual publish
// path directly against a fake Gate (mirroring tools_createpr_test.go's
// own convention for adapter-dependent tools), since no real github
// adapter subprocess is spawned in this test suite.
func TestPublishPrHappyPathViaFakeGate(t *testing.T) {
	a := newTestApp(t)
	orchID, laneID := seedRunnableLane(t, a)
	cs := connect(t, a)
	leaseToken, revision := claimSeededLane(t, cs, orchID, laneID)

	ctx := context.Background()
	store, err := openDeliveryStore(ctx, a)
	if err != nil {
		t.Fatalf("openDeliveryStore: %v", err)
	}

	gate, fc := newCreatePrTestGate(t)
	fc.responses["github.createPullRequest"] = `{"normalized":{"number":101,"url":"https://github.com/acme/widgets/pull/101"}}`
	// GitHubPRProvider.Publish scopes the approval by req.RepoSlug (that is
	// what it passes as Gate.Call's runID), not an arbitrary run id.
	const runID = "acme/widgets"
	if _, err := gate.RequestApproval(runID, "github.createPullRequest", protocol.ApprovalRecordRequestedByPetruk); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := gate.Approve(runID, "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	out, err := publishPr(ctx, store, &fakeGateProvider{gate: gate}, PublishPrInput{
		OrchestrationId: orchID, LaneId: laneID, LeaseToken: leaseToken, ExpectedRevision: revision,
		RepoSlug: "acme/widgets", BaseBranch: "main", HeadBranch: "punakawan/widgets", Title: "Widgets", Body: "body",
	})
	if err != nil {
		t.Fatalf("publishPr: %v", err)
	}
	if out.Lane.PrNumber == nil || *out.Lane.PrNumber != 101 {
		t.Fatalf("expected pr_number 101, got %+v", out.Lane.PrNumber)
	}
	if out.Lane.PrUrl == nil || *out.Lane.PrUrl != "https://github.com/acme/widgets/pull/101" {
		t.Fatalf("expected pr_url to round-trip, got %+v", out.Lane.PrUrl)
	}
}

// TestStartRepairCycleEndToEnd drives the bounded repair loop over the
// real MCP wire: three repair cycles succeed and increment the count each
// time, and a fourth reports escalated=true as a normal, successful
// result rather than a tool-call error.
func TestStartRepairCycleEndToEnd(t *testing.T) {
	a := newTestApp(t)
	orchID, laneID := seedRunnableLane(t, a)
	cs := connect(t, a)
	leaseToken, revision := claimSeededLane(t, cs, orchID, laneID)

	var completeOut LaneOutput
	callTool(t, cs, "complete_lease", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"lease_token":       leaseToken,
		"expected_revision": revision,
	}, &completeOut)
	if completeOut.Lane.Status != protocol.DeliveryLaneStatusReview {
		t.Fatalf("expected review status, got %s", completeOut.Lane.Status)
	}

	ctx := context.Background()
	store, err := openDeliveryStore(ctx, a)
	if err != nil {
		t.Fatalf("openDeliveryStore: %v", err)
	}

	rev := completeOut.Lane.Revision
	for i := 1; i <= delivery.MaxRepairCycles; i++ {
		var out StartRepairCycleOutput
		callTool(t, cs, "start_repair_cycle", map[string]any{
			"orchestration_id":  orchID,
			"lane_id":           laneID,
			"expected_revision": rev,
			"reason":            "review found issues",
			"evidence_ids":      []string{"evidence-1"},
		}, &out)
		if out.Escalated {
			t.Fatalf("cycle %d: unexpected escalation, got %+v", i, out)
		}
		if out.Lane.RepairCycleCount == nil || *out.Lane.RepairCycleCount != i {
			t.Fatalf("cycle %d: expected repair_cycle_count %d, got %+v", i, i, out.Lane.RepairCycleCount)
		}
		if out.Lane.Status != protocol.DeliveryLaneStatusRunnable {
			t.Fatalf("cycle %d: expected runnable status, got %s", i, out.Lane.Status)
		}

		back, err := store.UpdateLaneStatus(ctx, delivery.NewID(), orchID, laneID, out.Lane.Revision, protocol.DeliveryLaneStatusReview)
		if err != nil {
			t.Fatalf("cycle %d: UpdateLaneStatus back to review: %v", i, err)
		}
		rev = back.Revision
	}

	var exhausted StartRepairCycleOutput
	callTool(t, cs, "start_repair_cycle", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"expected_revision": rev,
		"reason":            "still broken after three attempts",
	}, &exhausted)
	if !exhausted.Escalated {
		t.Fatalf("expected escalated=true on the fourth repair cycle, got %+v", exhausted)
	}
	if exhausted.Reason == "" {
		t.Fatal("expected the escalation reason to be reported")
	}
	if exhausted.Lane.EscalatedAt == nil {
		t.Fatal("expected escalated_at to be set on the lane")
	}
}

// TestVerificationAndMergeReadinessEndToEnd drives
// record_verification_dimension/record_ci_check/submit_review_conclusion/
// get_verification_matrix/check_merge_readiness over the real MCP wire,
// checking a lane moves from not-ready to ready as its gates are
// satisfied and its review conclusion is recorded.
func TestVerificationAndMergeReadinessEndToEnd(t *testing.T) {
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
	if _, err := store.SetDeliveryProfile(ctx, delivery.NewID(), delivery.NewID(), lane.ProjectId, delivery.ProfileInput{
		BaseBranch:        "main",
		VerificationGates: []string{"unit"},
	}); err != nil {
		t.Fatalf("SetDeliveryProfile: %v", err)
	}

	var notReady CheckMergeReadinessOutput
	callTool(t, cs, "check_merge_readiness", map[string]any{
		"orchestration_id": orchID,
		"lane_id":          laneID,
	}, &notReady)
	if notReady.Ready {
		t.Fatalf("expected not ready before any verification or review, got %+v", notReady)
	}

	var vdOut LaneOutput
	callTool(t, cs, "record_verification_dimension", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"expected_revision": lane.Revision,
		"name":              "unit",
		"status":            "passed",
		"evidence_id":       "evidence-1",
		"summary":           "unit tests passed",
	}, &vdOut)

	var matrixOut GetVerificationMatrixOutput
	callTool(t, cs, "get_verification_matrix", map[string]any{
		"orchestration_id": orchID,
		"lane_id":          laneID,
	}, &matrixOut)
	found := false
	for _, dim := range matrixOut.Matrix.Dimensions {
		if dim.Name == protocol.VerificationMatrixDimensionsElemNameUnit {
			found = true
			if dim.Status != protocol.VerificationMatrixDimensionsElemStatusPassed {
				t.Fatalf("expected unit dimension passed, got %+v", dim)
			}
		}
	}
	if !found {
		t.Fatal("expected a unit dimension in the matrix")
	}

	var ciOut LaneOutput
	callTool(t, cs, "record_ci_check", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"expected_revision": vdOut.Lane.Revision,
		"check": map[string]any{
			"external_id": "check-build",
			"name":        "build",
			"provider":    "github",
			"status":      "passed",
			"required":    true,
			"reported_at": "2026-01-01T00:00:00Z",
		},
	}, &ciOut)

	var reviewOut SubmitReviewConclusionOutput
	callTool(t, cs, "submit_review_conclusion", map[string]any{
		"orchestration_id":       orchID,
		"lane_id":                laneID,
		"expected_revision":      ciOut.Lane.Revision,
		"implementer_session_id": "session-implementer",
		"conclusion": map[string]any{
			// id/lane_id/recorded_at are required by protocol.ReviewConclusion's
			// own schema but are overwritten unconditionally by
			// RecordReviewConclusion - any placeholder value here is fine.
			"id":                              "placeholder",
			"lane_id":                         "placeholder",
			"recorded_at":                     "2026-01-01T00:00:00Z",
			"outcome":                         "approved",
			"reviewer_worker_id":              "worker-1",
			"reviewer_session_id":             "session-reviewer",
			"independence_level":              "different_session",
			"evidence_ids":                    []string{"evidence-1"},
			"blocking_finding_ids":            []string{},
			"verification_matrix_computed_at": "2026-01-01T00:00:00Z",
		},
	}, &reviewOut)
	if reviewOut.Conclusion.Outcome != protocol.ReviewConclusionOutcomeApproved {
		t.Fatalf("expected approved outcome, got %+v", reviewOut.Conclusion)
	}

	var ready CheckMergeReadinessOutput
	callTool(t, cs, "check_merge_readiness", map[string]any{
		"orchestration_id": orchID,
		"lane_id":          laneID,
	}, &ready)
	if !ready.Ready {
		t.Fatalf("expected ready once unit passed and review approved, got %+v", ready)
	}
	if len(ready.FailingGates) != 0 {
		t.Fatalf("expected no failing gates, got %v", ready.FailingGates)
	}
}
