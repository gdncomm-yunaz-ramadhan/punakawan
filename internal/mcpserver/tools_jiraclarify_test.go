package mcpserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/internal/jiraworkflow"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeAtlassianCaller stands in for a real spawned adapter-atlassian
// process, mirroring internal/adapters/gate_test.go's fakeCaller pattern -
// exercising this handler's logic does not require live Jira credentials.
type fakeAtlassianCaller struct {
	calls     []map[string]any
	responses map[string]string // raw JSON per op name, defaulting to {"ok":true}
}

func (f *fakeAtlassianCaller) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	args, _ := params.(map[string]any)
	f.calls = append(f.calls, args)
	if op, _ := args["op"].(string); op != "" {
		if resp, ok := f.responses[op]; ok {
			return json.RawMessage(resp), nil
		}
	}
	return json.RawMessage(`{"ok":true}`), nil
}

func approvalRequired() *protocol.AdapterManifestOperationsValueApproval {
	v := protocol.AdapterManifestOperationsValueApprovalRequired
	return &v
}

func atlassianTestManifest() protocol.AdapterManifest {
	return protocol.AdapterManifest{
		Id:       "atlassian",
		Name:     "atlassian",
		Version:  "0.1.0",
		Protocol: "punakawan.adapter/v1",
		Runtime:  protocol.AdapterManifestRuntimeNode,
		Provides: []string{"jira"},
		Permissions: protocol.AdapterManifestPermissions{
			Network:    protocol.AdapterManifestPermissionsNetwork{Hosts: []string{"api.atlassian.com"}},
			Filesystem: protocol.AdapterManifestPermissionsFilesystem{Read: []string{}, Write: []string{}},
			Secrets:    []string{},
		},
		Operations: protocol.AdapterManifestOperations{
			"atlassian.addJiraComment":             {SideEffect: true, Approval: approvalRequired()},
			"atlassian.getTransitionsForJiraIssue": {SideEffect: false},
			"atlassian.transitionJiraIssue":        {SideEffect: true, Approval: approvalRequired()},
		},
	}
}

func newJiraClarifyTestGate(t *testing.T, transitionsJSON string) (*adapters.Gate, *fakeAtlassianCaller) {
	t.Helper()
	store, err := approvals.Open(t.TempDir())
	if err != nil {
		t.Fatalf("approvals.Open: %v", err)
	}
	fc := &fakeAtlassianCaller{responses: map[string]string{"atlassian.getTransitionsForJiraIssue": transitionsJSON}}
	return adapters.NewGate("atlassian", atlassianTestManifest(), fc, store), fc
}

const twoTransitionsJSON = `{"transitions":[
	{"id":"11","name":"Send back","toStatus":{"id":"10001","name":"Sent Back to Product Review"}},
	{"id":"21","name":"Start progress","toStatus":{"id":"10002","name":"In Progress"}}
]}`

func TestRequestJiraClarificationPostsCommentAndTransitions(t *testing.T) {
	gate, fc := newJiraClarifyTestGate(t, twoTransitionsJSON)
	cfg := &jiraworkflow.Config{ClarificationStatus: "Sent Back to Product Review"}
	in := RequestJiraClarificationInput{
		RunId:        "run-1",
		IssueIdOrKey: "PAY-1",
		CommentBody:  "Which refund policy applies?",
		RequestedBy:  "semar",
	}

	if _, err := gate.RequestApproval(in.RunId, "atlassian.addJiraComment", protocol.ApprovalRecordRequestedBySemar); err != nil {
		t.Fatalf("RequestApproval addJiraComment: %v", err)
	}
	if err := gate.Approve(in.RunId, "ygrip"); err != nil {
		t.Fatalf("Approve addJiraComment: %v", err)
	}

	out, err := requestJiraClarification(context.Background(), nil, gate, cfg, in)
	if err != nil {
		t.Fatalf("requestJiraClarification: %v", err)
	}
	if !out.CommentPosted {
		t.Error("CommentPosted = false, want true")
	}
	if !out.TransitionApplied {
		t.Error("TransitionApplied = false, want true")
	}
	if out.TransitionId != "11" {
		t.Errorf("TransitionId = %q, want %q", out.TransitionId, "11")
	}
	if out.ToStatus != "Sent Back to Product Review" {
		t.Errorf("ToStatus = %q, want %q", out.ToStatus, "Sent Back to Product Review")
	}

	var sawComment, sawTransition bool
	for _, c := range fc.calls {
		switch c["op"] {
		case "atlassian.addJiraComment":
			sawComment = true
			if c["commentBody"] != in.CommentBody {
				t.Errorf("commentBody = %v, want %q", c["commentBody"], in.CommentBody)
			}
		case "atlassian.transitionJiraIssue":
			sawTransition = true
			if c["transitionId"] != "11" {
				t.Errorf("transitionId = %v, want 11", c["transitionId"])
			}
		}
	}
	if !sawComment || !sawTransition {
		t.Fatalf("calls = %+v, want addJiraComment and transitionJiraIssue", fc.calls)
	}
}

func TestRequestJiraClarificationSkipsTransitionWhenUnconfigured(t *testing.T) {
	gate, fc := newJiraClarifyTestGate(t, twoTransitionsJSON)
	cfg := &jiraworkflow.Config{} // no clarification_status configured
	in := RequestJiraClarificationInput{RunId: "run-1", IssueIdOrKey: "PAY-1", CommentBody: "hi", RequestedBy: "semar"}

	if _, err := gate.RequestApproval(in.RunId, "atlassian.addJiraComment", protocol.ApprovalRecordRequestedBySemar); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := gate.Approve(in.RunId, "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	out, err := requestJiraClarification(context.Background(), nil, gate, cfg, in)
	if err != nil {
		t.Fatalf("requestJiraClarification: %v", err)
	}
	if !out.CommentPosted {
		t.Error("CommentPosted = false, want true")
	}
	if out.TransitionApplied {
		t.Error("TransitionApplied = true, want false when no clarification_status is configured")
	}
	for _, c := range fc.calls {
		if c["op"] == "atlassian.getTransitionsForJiraIssue" || c["op"] == "atlassian.transitionJiraIssue" {
			t.Fatalf("unexpected transition-related call: %+v", c)
		}
	}
}

func TestRequestJiraClarificationFailsClearlyWhenNoMatchingTransition(t *testing.T) {
	gate, _ := newJiraClarifyTestGate(t, twoTransitionsJSON)
	cfg := &jiraworkflow.Config{ClarificationStatus: "Does Not Exist"}
	in := RequestJiraClarificationInput{RunId: "run-1", IssueIdOrKey: "PAY-1", CommentBody: "hi", RequestedBy: "semar"}

	if _, err := gate.RequestApproval(in.RunId, "atlassian.addJiraComment", protocol.ApprovalRecordRequestedBySemar); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := gate.Approve(in.RunId, "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	out, err := requestJiraClarification(context.Background(), nil, gate, cfg, in)
	if err == nil {
		t.Fatal("expected an error when no transition matches the configured clarification status")
	}
	if !out.CommentPosted {
		t.Error("CommentPosted = false, want true (comment still posts even if the transition fails)")
	}
	if out.TransitionApplied {
		t.Error("TransitionApplied = true, want false")
	}
}

func TestRequestJiraClarificationFailsWithoutApproval(t *testing.T) {
	gate, _ := newJiraClarifyTestGate(t, twoTransitionsJSON)
	cfg := &jiraworkflow.Config{ClarificationStatus: "Sent Back to Product Review"}
	in := RequestJiraClarificationInput{RunId: "run-1", IssueIdOrKey: "PAY-1", CommentBody: "hi", RequestedBy: "semar"}

	if _, err := requestJiraClarification(context.Background(), nil, gate, cfg, in); err == nil {
		t.Fatal("expected an error when addJiraComment has not been approved")
	}
}
