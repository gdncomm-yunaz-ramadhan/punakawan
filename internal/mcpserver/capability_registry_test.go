package mcpserver

import "testing"

func TestCapabilityRegistryMatchesRegistration(t *testing.T) {
	a := newTestApp(t)
	reg := CapabilityRegistry(a)
	want := []string{
		"upsert_project", "list_projects",
		"save_workflow", "get_workflow", "list_workflows", "invoke_workflow",
		"plan_save", "plan_get",
		"start_delivery", "get_delivery", "answer_delivery_question", "log_delivery_work",
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
