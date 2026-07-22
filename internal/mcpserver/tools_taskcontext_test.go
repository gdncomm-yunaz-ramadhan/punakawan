package mcpserver

import (
	"context"
	"testing"
)

// TestBuildTaskContextResumeInheritsOmittedFields covers punokawan-d87: a
// second build_task_context call for the same run_id/task_id that omits
// task_scope/task_acceptance_criteria/etc. must inherit them from the
// task.yaml evidence the first call wrote, instead of coming back empty.
func TestBuildTaskContextResumeInheritsOmittedFields(t *testing.T) {
	requireDolt(t)

	a := newTestApp(t)
	seedRequirement(t, a, "pkw:req/smoke/REQ-1", "Refund approved order")

	handler := buildTaskContextHandler(a)

	_, first, err := handler(context.Background(), nil, BuildTaskContextInput{
		TaskId:                        "bd-task-1",
		RequirementId:                 "pkw:req/smoke/REQ-1",
		RunId:                         "run-1",
		TaskScope:                     "Implement the refund flow",
		TaskAcceptanceCriteria:        []string{"Refund settles same day"},
		TaskExpectedFilesOrComponents: []string{"internal/refund/service.go"},
	})
	if err != nil {
		t.Fatalf("first build_task_context: %v", err)
	}
	if first.TaskDefinition.Scope != "Implement the refund flow" {
		t.Fatalf("first Scope = %q", first.TaskDefinition.Scope)
	}

	// Resume: only RequiredTests actually changed this round.
	_, second, err := handler(context.Background(), nil, BuildTaskContextInput{
		TaskId:        "bd-task-1",
		RequirementId: "pkw:req/smoke/REQ-1",
		RunId:         "run-1",
		RequiredTests: []string{"TestRefundService_Settle"},
	})
	if err != nil {
		t.Fatalf("second build_task_context: %v", err)
	}

	if second.TaskDefinition.Scope != first.TaskDefinition.Scope {
		t.Errorf("Scope = %q, want inherited %q", second.TaskDefinition.Scope, first.TaskDefinition.Scope)
	}
	if len(second.TaskDefinition.AcceptanceCriteria) != 1 || second.TaskDefinition.AcceptanceCriteria[0] != "Refund settles same day" {
		t.Errorf("AcceptanceCriteria = %v, want inherited from the first call", second.TaskDefinition.AcceptanceCriteria)
	}
	if len(second.TaskDefinition.ExpectedFilesOrComponents) != 1 || second.TaskDefinition.ExpectedFilesOrComponents[0] != "internal/refund/service.go" {
		t.Errorf("ExpectedFilesOrComponents = %v, want inherited from the first call", second.TaskDefinition.ExpectedFilesOrComponents)
	}
	if len(second.RequiredTests) != 1 || second.RequiredTests[0] != "TestRefundService_Settle" {
		t.Errorf("RequiredTests = %v, want the explicitly-supplied value for this round", second.RequiredTests)
	}
}
