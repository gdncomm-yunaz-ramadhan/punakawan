package mcpserver

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// seedRequirementWithStatus mirrors tools_execution_m6_test.go's
// seedRequirement, but with a caller-supplied status - needed here to
// simulate a Jira-sourced requirement whose raw status name (e.g. "Sent
// Back to Product Review") drives the skip check, rather than the fixed
// "active" seedRequirement always uses.
func seedRequirementWithStatus(t *testing.T, a *app.App, id, title, status string) {
	t.Helper()
	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	err = store.Put(protocol.KnowledgeRecord{
		Id:     id,
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: status,
		Title:  title,
		Source: protocol.KnowledgeRecordSource{
			Provider:    "jira",
			RetrievedAt: time.Now().UTC(),
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodImported,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State: protocol.KnowledgeRecordValidityStateObserved,
		},
	})
	if err != nil {
		t.Fatalf("seed requirement: %v", err)
	}
}

func writeJiraWorkflowConfig(t *testing.T, a *app.App, yaml string) {
	t.Helper()
	if err := os.WriteFile(a.Workspace.JiraWorkflowPath(), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write jira-workflow.yaml: %v", err)
	}
}

func TestCheckJiraSkippableEndToEnd(t *testing.T) {
	requireDolt(t)

	a := newTestApp(t)
	writeJiraWorkflowConfig(t, a, "skip_statuses:\n  - \"Won't Do\"\n  - \"Sent Back to Product Review\"\n")
	seedRequirementWithStatus(t, a, "pkw:req/smoke/REQ-1", "Refund approved order", "Sent Back to Product Review")
	seedRequirementWithStatus(t, a, "pkw:req/smoke/REQ-2", "Refund declined order", "In Progress")

	cs := connect(t, a)

	var skippable CheckJiraSkippableOutput
	callTool(t, cs, "check_jira_skippable", map[string]any{"requirement_id": "pkw:req/smoke/REQ-1"}, &skippable)
	if !skippable.Skippable {
		t.Errorf("REQ-1 (status %q): Skippable = false, want true", skippable.Status)
	}

	var notSkippable CheckJiraSkippableOutput
	callTool(t, cs, "check_jira_skippable", map[string]any{"requirement_id": "pkw:req/smoke/REQ-2"}, &notSkippable)
	if notSkippable.Skippable {
		t.Errorf("REQ-2 (status %q): Skippable = true, want false", notSkippable.Status)
	}
}

func TestCheckJiraSkippableWithoutConfigNeverSkips(t *testing.T) {
	requireDolt(t)

	a := newTestApp(t)
	seedRequirementWithStatus(t, a, "pkw:req/smoke/REQ-1", "Refund approved order", "Sent Back to Product Review")

	cs := connect(t, a)
	var out CheckJiraSkippableOutput
	callTool(t, cs, "check_jira_skippable", map[string]any{"requirement_id": "pkw:req/smoke/REQ-1"}, &out)
	if out.Skippable {
		t.Error("Skippable = true, want false when no jira-workflow.yaml is configured")
	}
}

const createSubtaskResultJSON = `{
	"created": [{"key": "PAY-101", "summary": "Write migration", "status": "Open"}],
	"skipped": [{"summary": "Add API endpoint", "existingKey": "PAY-99"}]
}`

func TestSyncJiraSubtasksCreatesAndReportsSkipped(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, syncSubtaskManifest())
	in := SyncJiraSubtasksInput{
		RunId:         "run-1",
		ParentKey:     "PAY-1",
		ProjectKey:    "PAY",
		IssueTypeName: "Subtask",
		Candidates: []SyncJiraSubtasksCandidate{
			{Summary: "Write migration"},
			{Summary: "Add API endpoint"},
		},
		RequestedBy: "petruk",
	}
	fc.responses = map[string]string{"atlassian.createJiraSubtask": createSubtaskResultJSON}

	if _, err := gate.RequestApproval(in.RunId, "atlassian.createJiraSubtask", protocol.ApprovalRecordRequestedByPetruk); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := gate.Approve(in.RunId, "atlassian.createJiraSubtask", "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	out, err := syncJiraSubtasks(context.Background(), gate, in)
	if err != nil {
		t.Fatalf("syncJiraSubtasks: %v", err)
	}
	if len(out.Created) != 1 || out.Created[0].Key != "PAY-101" {
		t.Errorf("Created = %+v, want one entry keyed PAY-101", out.Created)
	}
	if len(out.Skipped) != 1 || out.Skipped[0].ExistingKey != "PAY-99" {
		t.Errorf("Skipped = %+v, want one entry existing_key PAY-99", out.Skipped)
	}

	if len(fc.calls) != 1 {
		t.Fatalf("calls = %+v, want exactly one createJiraSubtask call", fc.calls)
	}
	candidates, _ := fc.calls[0]["candidates"].([]map[string]any)
	if len(candidates) != 2 {
		t.Fatalf("candidates sent = %+v, want 2", fc.calls[0]["candidates"])
	}
}

func TestSyncJiraSubtasksFailsWithoutApproval(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, syncSubtaskManifest())
	in := SyncJiraSubtasksInput{RunId: "run-1", ParentKey: "PAY-1", ProjectKey: "PAY", IssueTypeName: "Subtask", RequestedBy: "petruk"}

	if _, err := syncJiraSubtasks(context.Background(), gate, in); err == nil {
		t.Fatal("expected an error when createJiraSubtask has not been approved")
	}
}

// syncSubtaskManifest reuses atlassianTestManifest's existing
// approval-required addJiraComment entry's shape for createJiraSubtask
// (both are side-effecting, approval-required ops), avoiding having to
// re-spell the generated anonymous struct type.
func syncSubtaskManifest() protocol.AdapterManifest {
	m := atlassianTestManifest()
	m.Operations["atlassian.createJiraSubtask"] = m.Operations["atlassian.addJiraComment"]
	return m
}

// newJiraClarifyTestGateWithManifest is newJiraClarifyTestGate generalized
// to accept a caller-supplied manifest, since sync_jira_subtasks gates a
// different operation than request_jira_clarification does.
func newJiraClarifyTestGateWithManifest(t *testing.T, manifest protocol.AdapterManifest) (*adapters.Gate, *fakeAtlassianCaller) {
	t.Helper()
	store, err := approvals.Open(t.TempDir())
	if err != nil {
		t.Fatalf("approvals.Open: %v", err)
	}
	fc := &fakeAtlassianCaller{}
	return adapters.NewGate("atlassian", manifest, fc, store), fc
}
