package agent

import (
	"testing"

	"github.com/ygrip/punakawan/internal/capability"
)

func TestRealManifestsLoadAndValidate(t *testing.T) {
	reg, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	specs := reg.List()
	if got, want := len(specs), 4; got != want {
		t.Fatalf("len(List()) = %d, want %d", got, want)
	}

	wantIDs := map[string]bool{"semar": true, "gareng": true, "petruk": true, "bagong": true}
	for _, spec := range specs {
		if !wantIDs[spec.ID] {
			t.Errorf("unexpected role id %q", spec.ID)
		}
		delete(wantIDs, spec.ID)
	}
	if len(wantIDs) > 0 {
		for id := range wantIDs {
			t.Errorf("missing expected role id %q", id)
		}
	}

	schemaChecker, err := NewKnowledgeSchemaChecker()
	if err != nil {
		t.Fatalf("NewKnowledgeSchemaChecker: %v", err)
	}

	// Build a real *capability.Registry populated with exactly the tool
	// names internal/mcpserver/tools_public.go's registerPublicTools
	// actually registers (copied from that function, not from the
	// manifests themselves), so a typo'd tool name in any
	// prompts/*/agent.yaml fails this test instead of trivially
	// validating against its own claims.
	realToolNames := []string{
		"list_adapter_operations", "call_adapter_operation", "upsert_project", "list_projects",
		"save_workflow", "get_workflow", "list_workflows", "invoke_workflow",
		"plan_save", "plan_get", "start_delivery", "start_delivery_session",
		"checkpoint_delivery_session", "ingest_delivery_usage_snapshot", "finalize_delivery_session",
		"report_delivery_usage", "report_delivery_progress", "assess_jira_delivery",
		"hydrate_jira_delivery", "hydrate_github_pull_request", "propose_github_pr_review",
		"get_github_pr_review", "submit_github_pr_review", "map_delivery_work_item",
		"get_delivery", "answer_delivery_question", "log_delivery_work", "retry_worklog_sync",
		"cancel_jira_write_intent", "cancel_delivery", "complete_delivery",
	}
	capReg := capability.NewRegistry()
	for _, name := range realToolNames {
		capReg.Add(capability.Descriptor{Name: name, Source: capability.SourceMCP})
	}

	if err := Validate(specs, schemaChecker, capReg); err != nil {
		t.Fatalf("Validate real manifests: %v", err)
	}
}
