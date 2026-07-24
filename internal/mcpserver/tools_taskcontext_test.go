package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeProjectMetadata writes a .punakawan/project.yaml at root carrying the
// given metadata block, so build_task_context / selectProjectMetadata have a
// real project to load.
func writeProjectMetadata(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".punakawan")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir .punakawan: %v", err)
	}
	content := "version: punakawan.project/v1\n" +
		"id: smoke\n" +
		"name: Smoke\n" +
		"revision: 1\n" +
		"metadata:\n" +
		"  - key: jira.project_key\n" +
		"    description: Jira project key used for this project.\n" +
		"    value: TRF\n" +
		"  - key: jira.board_id\n" +
		"    description: Jira board that defines the active and next sprint for this project.\n" +
		"    value: 127\n" +
		"  - key: deploy.region\n" +
		"    description: Primary deployment region.\n" +
		"    value: ap-southeast-1\n"
	if err := os.WriteFile(filepath.Join(dir, "project.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write project.yaml: %v", err)
	}
}

// TestSelectProjectMetadataPriorityAndRender covers §4.4 at the selector-wiring
// level without needing the knowledge store: explicit requested keys come
// first, then the capability namespace, and each entry renders in the §4.4
// key/description/Value block format.
func TestSelectProjectMetadataPriorityAndRender(t *testing.T) {
	root := t.TempDir()
	writeProjectMetadata(t, root)

	entries, err := selectProjectMetadata(root, "jira", "", []string{"deploy.region"})
	if err != nil {
		t.Fatalf("selectProjectMetadata: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(entries), entries)
	}

	// 1. Explicitly requested key first, 2. capability namespace after.
	if entries[0].Key != "deploy.region" {
		t.Errorf("entries[0].Key = %q, want deploy.region (requested keys take priority)", entries[0].Key)
	}
	if entries[1].Key != "jira.project_key" || entries[2].Key != "jira.board_id" {
		t.Errorf("capability-namespace order = %q,%q, want jira.project_key,jira.board_id", entries[1].Key, entries[2].Key)
	}

	want := "jira.project_key\n  Jira project key used for this project.\n  Value: TRF"
	if entries[1].Rendered != want {
		t.Errorf("Rendered = %q, want %q", entries[1].Rendered, want)
	}
}

// TestSelectProjectMetadataNoProjectYAML covers the safe-when-absent contract:
// with no project.yaml, project.Load synthesizes a zero-metadata project, so
// selection returns nothing to inject.
func TestSelectProjectMetadataNoProjectYAML(t *testing.T) {
	root := t.TempDir()
	entries, err := selectProjectMetadata(root, "jira", "", nil)
	if err != nil {
		t.Fatalf("selectProjectMetadata: %v", err)
	}
	if entries != nil {
		t.Errorf("entries = %+v, want nil (nothing to inject without project metadata)", entries)
	}
}

// TestBuildTaskContextInjectsProjectMetadata covers the end-to-end wiring
// (§4.4): a workspace with project metadata and a matching capability yields a
// build_task_context result whose ProjectMetadata carries only the relevant,
// selected entries.
func TestBuildTaskContextInjectsProjectMetadata(t *testing.T) {
	requireDolt(t)

	a := newTestApp(t)
	writeProjectMetadata(t, a.Workspace.Root)
	seedRequirement(t, a, "pkw:req/smoke/REQ-1", "Refund approved order")

	handler := buildTaskContextHandler(a)

	_, out, err := handler(context.Background(), nil, BuildTaskContextInput{
		TaskId:        "bd-task-1",
		RequirementId: "pkw:req/smoke/REQ-1",
		RunId:         "run-1",
		TaskScope:     "Implement the refund flow",
		Capability:    "jira",
	})
	if err != nil {
		t.Fatalf("build_task_context: %v", err)
	}

	if out.TaskDefinition.Scope != "Implement the refund flow" {
		t.Fatalf("Scope = %q, want the supplied value (embedded Context must survive)", out.TaskDefinition.Scope)
	}

	if len(out.ProjectMetadata) != 3 {
		t.Fatalf("ProjectMetadata len = %d, want 3: %+v", len(out.ProjectMetadata), out.ProjectMetadata)
	}
	if out.ProjectMetadata[0].Key != "jira.project_key" || out.ProjectMetadata[1].Key != "jira.board_id" {
		t.Errorf("capability jira should surface its namespace first, got %q,%q", out.ProjectMetadata[0].Key, out.ProjectMetadata[1].Key)
	}
	if !strings.Contains(out.ProjectMetadata[0].Rendered, "Value: TRF") {
		t.Errorf("rendered entry = %q, want it to carry Value: TRF", out.ProjectMetadata[0].Rendered)
	}
}

// TestBuildTaskContextNoProjectMetadata covers the additive-safety contract at
// the wiring level: newTestApp writes no project.yaml, so the built context is
// returned exactly as before with no project_metadata attached.
func TestBuildTaskContextNoProjectMetadata(t *testing.T) {
	requireDolt(t)

	a := newTestApp(t)
	seedRequirement(t, a, "pkw:req/smoke/REQ-1", "Refund approved order")

	handler := buildTaskContextHandler(a)

	_, out, err := handler(context.Background(), nil, BuildTaskContextInput{
		TaskId:        "bd-task-1",
		RequirementId: "pkw:req/smoke/REQ-1",
		RunId:         "run-1",
		TaskScope:     "Implement the refund flow",
		Capability:    "jira",
	})
	if err != nil {
		t.Fatalf("build_task_context: %v", err)
	}
	if out.ProjectMetadata != nil {
		t.Errorf("ProjectMetadata = %+v, want nil without project.yaml", out.ProjectMetadata)
	}
	if out.TaskDefinition.Scope != "Implement the refund flow" {
		t.Errorf("Scope = %q, want unchanged behavior", out.TaskDefinition.Scope)
	}
}

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
