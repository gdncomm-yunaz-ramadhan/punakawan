package mcpserver

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/internal/jiraworkflow"
	"github.com/ygrip/punakawan/internal/syncqueue"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func progressTestManifest() protocol.AdapterManifest {
	m := atlassianTestManifest()
	m.Operations["atlassian.editJiraIssueFields"] = m.Operations["atlassian.addJiraComment"]
	m.Operations["atlassian.addWorklog"] = m.Operations["atlassian.addJiraComment"]
	return m
}

func approveOp(t *testing.T, gate interface {
	RequestApproval(runID, op string, by protocol.ApprovalRecordRequestedBy) (protocol.ApprovalRecord, error)
	Approve(runID, approvedBy string) error
}, runID, op string) {
	t.Helper()
	if _, err := gate.RequestApproval(runID, op, protocol.ApprovalRecordRequestedByPetruk); err != nil {
		t.Fatalf("RequestApproval(%s): %v", op, err)
	}
	if err := gate.Approve(runID, "ygrip"); err != nil {
		t.Fatalf("Approve(%s): %v", op, err)
	}
}

func TestUpdateJiraTaskProgressDerivesEstimateFromStoryPoints(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, progressTestManifest())
	approveOp(t, gate, "run-1", "atlassian.editJiraIssueFields")

	points := 5.0
	in := UpdateJiraTaskProgressInput{RunId: "run-1", IssueIdOrKey: "PAY-1", StoryPoints: &points, RequestedBy: "petruk"}
	cfg := &jiraworkflow.Config{Estimation: jiraworkflow.EstimationConfig{PointsToHours: 4}}

	out, err := updateJiraTaskProgress(context.Background(), nil, gate, cfg, in)
	if err != nil {
		t.Fatalf("updateJiraTaskProgress: %v", err)
	}
	if !out.EstimateUpdated || out.EstimateHours != 20 {
		t.Fatalf("out = %+v, want EstimateUpdated=true EstimateHours=20", out)
	}
	if len(fc.calls) != 1 {
		t.Fatalf("calls = %+v, want exactly one editJiraIssueFields call", fc.calls)
	}
	fields, _ := fc.calls[0]["fields"].(map[string]any)
	tt, _ := fields["timetracking"].(map[string]any)
	if tt["originalEstimate"] != "20h" {
		t.Errorf("originalEstimate = %v, want 20h", tt["originalEstimate"])
	}
	if tt["remainingEstimate"] != "20h" {
		t.Errorf("remainingEstimate = %v, want 20h (no worklog given, so remaining = original)", tt["remainingEstimate"])
	}
	if out.RemainingEstimateHours != 20 {
		t.Errorf("RemainingEstimateHours = %v, want 20", out.RemainingEstimateHours)
	}
}

func TestUpdateJiraTaskProgressExplicitEstimateOverridesPoints(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, progressTestManifest())
	approveOp(t, gate, "run-1", "atlassian.editJiraIssueFields")

	points := 5.0
	explicit := 3.5
	in := UpdateJiraTaskProgressInput{RunId: "run-1", IssueIdOrKey: "PAY-1", StoryPoints: &points, OriginalEstimateHours: &explicit, RequestedBy: "petruk"}
	cfg := &jiraworkflow.Config{Estimation: jiraworkflow.EstimationConfig{PointsToHours: 4}}

	out, err := updateJiraTaskProgress(context.Background(), nil, gate, cfg, in)
	if err != nil {
		t.Fatalf("updateJiraTaskProgress: %v", err)
	}
	if out.EstimateHours != 3.5 {
		t.Fatalf("EstimateHours = %v, want 3.5 (explicit override, not points-derived 20)", out.EstimateHours)
	}
	fields, _ := fc.calls[0]["fields"].(map[string]any)
	tt, _ := fields["timetracking"].(map[string]any)
	if tt["originalEstimate"] != "3h 30m" {
		t.Errorf("originalEstimate = %v, want 3h 30m", tt["originalEstimate"])
	}
}

func TestUpdateJiraTaskProgressNoEstimateWhenRatioUnconfigured(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, progressTestManifest())

	points := 5.0
	in := UpdateJiraTaskProgressInput{RunId: "run-1", IssueIdOrKey: "PAY-1", StoryPoints: &points, RequestedBy: "petruk"}
	cfg := &jiraworkflow.Config{} // points_to_hours not configured

	out, err := updateJiraTaskProgress(context.Background(), nil, gate, cfg, in)
	if err != nil {
		t.Fatalf("updateJiraTaskProgress: %v", err)
	}
	if out.EstimateUpdated {
		t.Error("EstimateUpdated = true, want false when points_to_hours is unconfigured")
	}
	if out.EstimateSkipReason == "" {
		t.Error("EstimateSkipReason is empty, want an explanation since story_points was given but no ratio is configured")
	}
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want none", fc.calls)
	}
}

func TestUpdateJiraTaskProgressRemainingEstimateSubtractsSameCallWorklog(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, progressTestManifest())
	approveOp(t, gate, "run-1", "atlassian.editJiraIssueFields")

	explicit := 10.0
	worklog := 3.0
	in := UpdateJiraTaskProgressInput{RunId: "run-1", IssueIdOrKey: "PAY-1", OriginalEstimateHours: &explicit, WorklogHours: &worklog, RequestedBy: "petruk"}
	cfg := &jiraworkflow.Config{}

	out, err := updateJiraTaskProgress(context.Background(), nil, gate, cfg, in)
	if err != nil {
		t.Fatalf("updateJiraTaskProgress: %v", err)
	}
	if out.RemainingEstimateHours != 7 {
		t.Fatalf("RemainingEstimateHours = %v, want 7 (10h original - 3h worklog logged in this same call)", out.RemainingEstimateHours)
	}
	fields, _ := fc.calls[0]["fields"].(map[string]any)
	tt, _ := fields["timetracking"].(map[string]any)
	if tt["remainingEstimate"] != "7h" {
		t.Errorf("remainingEstimate = %v, want 7h", tt["remainingEstimate"])
	}
}

func TestUpdateJiraTaskProgressRemainingEstimateClampsAtZeroWhenWorklogExceedsEstimate(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, progressTestManifest())
	approveOp(t, gate, "run-1", "atlassian.editJiraIssueFields")

	explicit := 2.0
	worklog := 5.0
	in := UpdateJiraTaskProgressInput{RunId: "run-1", IssueIdOrKey: "PAY-1", OriginalEstimateHours: &explicit, WorklogHours: &worklog, RequestedBy: "petruk"}
	cfg := &jiraworkflow.Config{}

	out, err := updateJiraTaskProgress(context.Background(), nil, gate, cfg, in)
	if err != nil {
		t.Fatalf("updateJiraTaskProgress: %v", err)
	}
	if out.RemainingEstimateHours != 0 {
		t.Fatalf("RemainingEstimateHours = %v, want 0 (worklog exceeds original estimate, clamp instead of going negative)", out.RemainingEstimateHours)
	}
	fields, _ := fc.calls[0]["fields"].(map[string]any)
	tt, _ := fields["timetracking"].(map[string]any)
	if tt["remainingEstimate"] != "0m" {
		t.Errorf("remainingEstimate = %v, want 0m", tt["remainingEstimate"])
	}
}

func TestUpdateJiraTaskProgressWorklogAndComment(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, progressTestManifest())
	approveOp(t, gate, "run-1", "atlassian.addWorklog")

	worklog := 1.5
	in := UpdateJiraTaskProgressInput{RunId: "run-1", IssueIdOrKey: "PAY-1", WorklogHours: &worklog, Comment: "Done", RequestedBy: "petruk"}
	cfg := &jiraworkflow.Config{}

	out, err := updateJiraTaskProgress(context.Background(), nil, gate, cfg, in)
	if err != nil {
		t.Fatalf("updateJiraTaskProgress: %v", err)
	}
	if !out.WorklogAdded || !out.CommentPosted {
		t.Fatalf("out = %+v, want WorklogAdded and CommentPosted both true", out)
	}
	if out.EstimateUpdated {
		t.Error("EstimateUpdated = true, want false (no estimate input given)")
	}
	if out.EstimateSkipReason != "" {
		t.Errorf("EstimateSkipReason = %q, want empty (no estimate was requested at all)", out.EstimateSkipReason)
	}

	var sawWorklog, sawComment bool
	for _, c := range fc.calls {
		switch c["op"] {
		case "atlassian.addWorklog":
			sawWorklog = true
			if c["timeSpentSeconds"] != 5400 {
				t.Errorf("timeSpentSeconds = %v, want 5400 (1.5h)", c["timeSpentSeconds"])
			}
		case "atlassian.addJiraComment":
			sawComment = true
			if c["commentBody"] != "Done" {
				t.Errorf("commentBody = %v, want Done", c["commentBody"])
			}
		}
	}
	if !sawWorklog || !sawComment {
		t.Fatalf("calls = %+v, want addWorklog and addJiraComment", fc.calls)
	}
}

func TestUpdateJiraTaskProgressFailsWithoutApproval(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, progressTestManifest())
	worklog := 1.0
	in := UpdateJiraTaskProgressInput{RunId: "run-1", IssueIdOrKey: "PAY-1", WorklogHours: &worklog, RequestedBy: "petruk"}
	cfg := &jiraworkflow.Config{}

	if _, err := updateJiraTaskProgress(context.Background(), nil, gate, cfg, in); err == nil {
		t.Fatal("expected an error when addWorklog has not been approved")
	}
}

func TestUpdateJiraTaskProgressEnqueuesFailureForRetry(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, progressTestManifest())
	approveOp(t, gate, "run-1", "atlassian.addWorklog")
	fc.failOps = map[string]bool{"atlassian.addWorklog": true}

	queue, err := syncqueue.Open(t.TempDir())
	if err != nil {
		t.Fatalf("syncqueue.Open: %v", err)
	}
	gate.SetSyncQueue(queue)

	worklog := 1.0
	in := UpdateJiraTaskProgressInput{RunId: "run-1", IssueIdOrKey: "PAY-1", WorklogHours: &worklog, RequestedBy: "petruk"}
	cfg := &jiraworkflow.Config{}

	if _, err := updateJiraTaskProgress(context.Background(), nil, gate, cfg, in); err == nil {
		t.Fatal("expected the simulated adapter failure to surface")
	}

	pending, err := queue.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 || pending[0].Op != "atlassian.addWorklog" || pending[0].IssueIdOrKey != "PAY-1" {
		t.Fatalf("Pending = %+v, want one queued atlassian.addWorklog failure for PAY-1", pending)
	}
}

func TestFormatJiraDuration(t *testing.T) {
	cases := []struct {
		hours float64
		want  string
	}{
		{0.5, "30m"},
		{6, "6h"},
		{6.5, "6h 30m"},
		{0, "0m"},
	}
	for _, tc := range cases {
		if got := formatJiraDuration(tc.hours); got != tc.want {
			t.Errorf("formatJiraDuration(%v) = %q, want %q", tc.hours, got, tc.want)
		}
	}
}
