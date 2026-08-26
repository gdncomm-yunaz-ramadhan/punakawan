package mcpserver

import "testing"

func TestCapabilityRegistryMatchesRegistration(t *testing.T) {
	a := newTestApp(t)
	reg := CapabilityRegistry(a)
	want := []string{
		"upsert_project", "list_projects",
		"save_workflow", "get_workflow", "list_workflows", "invoke_workflow",
		"plan_save", "plan_get",
		"start_delivery", "resolve_jira_delivery", "start_delivery_session", "checkpoint_delivery_session",
		"report_delivery_usage", "report_delivery_progress", "assess_jira_delivery", "hydrate_jira_delivery",
		"queue_jira_write", "execute_jira_writes", "map_delivery_work_item", "get_delivery",
		"answer_delivery_question", "log_delivery_work",
		"cancel_delivery", "approve_project_delivery",
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
