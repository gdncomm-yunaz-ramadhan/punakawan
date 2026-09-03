package mcpserver

import "testing"

func TestCapabilityRegistryMatchesRegistration(t *testing.T) {
	a := newTestApp(t)
	reg := CapabilityRegistry(a)
	want := []string{
		"role_list", "role_get",
		"list_adapter_operations", "call_adapter_operation",
		"upsert_project", "list_projects",
		"save_workflow", "get_workflow", "list_workflows", "invoke_workflow",
		"plan_save", "plan_get",
		"start_delivery", "start_delivery_session", "checkpoint_delivery_session",
		"ingest_delivery_usage_snapshot", "finalize_delivery_session",
		"report_delivery_usage", "report_delivery_progress", "assess_jira_delivery", "hydrate_jira_delivery",
		"hydrate_github_pull_request", "propose_github_pr_review", "get_github_pr_review", "submit_github_pr_review", "map_delivery_work_item", "get_delivery",
		"answer_delivery_question", "log_delivery_work", "retry_worklog_sync", "post_jira_comment",
		"cancel_delivery", "complete_delivery_lane", "complete_delivery",
	}
	if reg.Len() != len(want) {
		t.Fatalf("registry Len = %d, want %d", reg.Len(), len(want))
	}
	for _, name := range want {
		if !reg.Has(name) {
			t.Errorf("registry missing public tool %q", name)
		}
	}
}
