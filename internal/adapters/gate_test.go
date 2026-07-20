package adapters

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeCaller records every "execute" call it receives instead of talking to
// a real subprocess, so Gate's approval logic can be tested in isolation.
type fakeCaller struct {
	calls []map[string]any
}

func (f *fakeCaller) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, params.(map[string]any))
	return json.RawMessage(`{"ok":true}`), nil
}

func approvalRequired() *protocol.AdapterManifestOperationsValueApproval {
	v := protocol.AdapterManifestOperationsValueApprovalRequired
	return &v
}

func testManifest() protocol.AdapterManifest {
	return protocol.AdapterManifest{
		Id:       "atlassian",
		Name:     "atlassian",
		Version:  "0.1.0",
		Protocol: "punakawan.adapter/v1",
		Runtime:  protocol.AdapterManifestRuntimeNode,
		Provides: []string{"jira"},
		Permissions: protocol.AdapterManifestPermissions{
			Network:    protocol.AdapterManifestPermissionsNetwork{Hosts: []string{"mcp.atlassian.com"}},
			Filesystem: protocol.AdapterManifestPermissionsFilesystem{Read: []string{}, Write: []string{}},
			Secrets:    []string{},
		},
		Operations: protocol.AdapterManifestOperations{
			"atlassian.getJiraIssue": {SideEffect: false},
			"atlassian.addJiraComment": {
				SideEffect: true,
				Approval:   approvalRequired(),
			},
		},
	}
}

func newTestGate(t *testing.T) (*Gate, *fakeCaller) {
	t.Helper()
	store, err := approvals.Open(t.TempDir())
	if err != nil {
		t.Fatalf("approvals.Open: %v", err)
	}
	fc := &fakeCaller{}
	return NewGate("atlassian", testManifest(), fc, store), fc
}

func TestGateAllowsUnrestrictedOperation(t *testing.T) {
	g, fc := newTestGate(t)

	if _, err := g.Call(context.Background(), "run-1", "atlassian.getJiraIssue", map[string]any{"issueIdOrKey": "PAY-1"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(fc.calls) != 1 || fc.calls[0]["op"] != "atlassian.getJiraIssue" {
		t.Fatalf("calls = %+v", fc.calls)
	}
}

func TestGateBlocksApprovalRequiredOperationWithoutApproval(t *testing.T) {
	g, fc := newTestGate(t)

	if _, err := g.Call(context.Background(), "run-1", "atlassian.addJiraComment", map[string]any{"issueIdOrKey": "PAY-1"}); err == nil {
		t.Fatal("expected error for unapproved operation")
	}
	if len(fc.calls) != 0 {
		t.Fatalf("expected no adapter call, got %+v", fc.calls)
	}
}

func TestGateAllowsApprovalRequiredOperationOnceApproved(t *testing.T) {
	g, fc := newTestGate(t)

	if _, err := g.RequestApproval("run-1", "atlassian.addJiraComment", protocol.ApprovalRecordRequestedBySemar); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if _, err := g.Call(context.Background(), "run-1", "atlassian.addJiraComment", nil); err == nil {
		t.Fatal("expected error before approval is granted")
	}

	if err := g.Approve("run-1", "atlassian.addJiraComment", "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if _, err := g.Call(context.Background(), "run-1", "atlassian.addJiraComment", map[string]any{"commentBody": "hi"}); err != nil {
		t.Fatalf("Call after approval: %v", err)
	}
	if len(fc.calls) != 1 {
		t.Fatalf("calls = %+v", fc.calls)
	}
}

func TestGateDeniedOperationStaysBlocked(t *testing.T) {
	g, fc := newTestGate(t)

	if _, err := g.RequestApproval("run-1", "atlassian.addJiraComment", protocol.ApprovalRecordRequestedBySemar); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := g.Deny("run-1", "atlassian.addJiraComment", "ygrip"); err != nil {
		t.Fatalf("Deny: %v", err)
	}
	if _, err := g.Call(context.Background(), "run-1", "atlassian.addJiraComment", nil); err == nil {
		t.Fatal("expected error for denied operation")
	}
	if len(fc.calls) != 0 {
		t.Fatalf("expected no adapter call, got %+v", fc.calls)
	}
}

func TestOperationCategoryMapping(t *testing.T) {
	cases := map[string]protocol.ApprovalRecordOperation{
		"atlassian.updateConfluencePage": protocol.ApprovalRecordOperationConfluenceUpdate,
		"atlassian.createJiraIssue":      protocol.ApprovalRecordOperationIssueCreation,
		"atlassian.transitionJiraIssue":  protocol.ApprovalRecordOperationIssueTransition,
		"atlassian.addJiraComment":       protocol.ApprovalRecordOperationExternalWrite,
	}
	for op, want := range cases {
		if got := operationCategory(op); got != want {
			t.Errorf("operationCategory(%q) = %q, want %q", op, got, want)
		}
	}
}
