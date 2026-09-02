package mcpserver

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/delivery"
)

// TestJiraDeliveryCanBindAndLogWork walks the exact chain a delivery has
// to complete before any time is recorded against Jira: start with a
// source and a project, bind the lane's parent task to the issue, then
// log measured work on it.
//
// Every id this needs comes from start_delivery's own response, which is
// the point - the execution id and the requirement source id are required
// by map_delivery_work_item and used to be obtainable from no call at
// all. The bind itself also used to be impossible for a different reason:
// the captured source's canonical key omitted the tenant while the check
// recomputed it with the tenant included, so it never matched and
// log_delivery_work stayed blocked behind it.
func TestJiraDeliveryCanBindAndLogWork(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	var started StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"source": jiraSource("PAY-7001"),
		"projects": []map[string]any{{
			"slug":           "payments-api",
			"repository_url": "https://example.test/payments-api.git",
			"tasks":          []map[string]any{{"title": "raise the charge endpoint"}},
		}},
	}, &started)

	if len(started.View.Lanes) != 1 || len(started.RequirementSources) != 1 {
		t.Fatalf("Lanes = %+v, RequirementSources = %+v, want one of each to bind", started.View.Lanes, started.RequirementSources)
	}
	lane := started.View.Lanes[0]

	var mapped MapDeliveryWorkItemOutput
	callTool(t, cs, "map_delivery_work_item", map[string]any{
		"execution_id":          started.ExecutionId,
		"parent_task_id":        lane.ParentTaskID,
		"requirement_source_id": started.RequirementSources[0].Id,
		"jira_issue_key":        "PAY-7001",
	}, &mapped)
	if mapped.Mapping.ParentTaskID != lane.ParentTaskID || mapped.Mapping.JiraIssueKey != "PAY-7001" {
		t.Fatalf("Mapping = %+v, want the lane's parent task bound to PAY-7001", mapped.Mapping)
	}

	var logged LogDeliveryWorkOutput
	callTool(t, cs, "log_delivery_work", map[string]any{
		"orchestration_id": started.OrchestrationId,
		"lane_id":          lane.LaneID,
		"jira_issue_key":   "PAY-7001",
		"started_at":       time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339),
		"duration_seconds": 1800,
		"summary":          "raised the charge endpoint onto v2",
		"worklog_id":       "worklog-" + delivery.NewID(),
		"idempotency_key":  delivery.NewID(),
	}, &logged)

	if logged.WorkLog.DurationSeconds != 1800 || logged.WorkLog.ParentTaskID != lane.ParentTaskID {
		t.Fatalf("WorkLog = %+v, want 1800s recorded against the bound parent task", logged.WorkLog)
	}
	if logged.View.WorkLogSeconds != 1800 {
		t.Fatalf("view.worklog_seconds = %d, want 1800", logged.View.WorkLogSeconds)
	}

	// get_delivery must expose the same source id, so a caller resuming a
	// delivery it did not start can bind further work items too.
	var got DeliveryViewOutput
	callTool(t, cs, "get_delivery", map[string]any{"orchestration_id": started.OrchestrationId}, &got)
	if len(got.View.RequirementSources) != 1 || got.View.RequirementSources[0].Id != started.RequirementSources[0].Id {
		t.Fatalf("get_delivery requirement_sources = %+v, want the captured source", got.View.RequirementSources)
	}

	// Recording work against an issue counts as touching it. Nothing had
	// ever called TouchJiraWorkItem, so touch_count was structurally
	// always 0 and first/last_touched_at always empty - a projected field
	// that could not say anything.
	if got.View.Lifecycle == nil || len(got.View.Lifecycle.WorkItems) != 1 {
		t.Fatalf("lifecycle work items = %+v, want the one bound mapping", got.View.Lifecycle)
	}
	item := got.View.Lifecycle.WorkItems[0]
	if item.TouchCount != 1 {
		t.Fatalf("touch_count = %d, want 1 after logging work against %s", item.TouchCount, item.JiraIssueKey)
	}
	if item.LastTouchedAt == nil || item.FirstTouchedAt == nil {
		t.Fatalf("work item = %+v, want first/last touched timestamps recorded", item)
	}
}
