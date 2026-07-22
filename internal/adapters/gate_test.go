package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/internal/syncqueue"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeCaller records every "execute" call it receives instead of talking to
// a real subprocess, so Gate's approval logic can be tested in isolation.
// failOps, if set, makes Call return an error instead of a canned success
// for any op named in it, so Gate's sync-queue-on-failure behavior
// (punokawan-nbz) can be exercised without a real adapter failure.
type fakeCaller struct {
	calls   []map[string]any
	failOps map[string]bool
}

func (f *fakeCaller) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	args := params.(map[string]any)
	f.calls = append(f.calls, args)
	if op, _ := args["op"].(string); f.failOps[op] {
		return nil, fmt.Errorf("simulated adapter failure for %q", op)
	}
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
			Network:    protocol.AdapterManifestPermissionsNetwork{Hosts: []string{"api.atlassian.com"}},
			Filesystem: protocol.AdapterManifestPermissionsFilesystem{Read: []string{}, Write: []string{}},
			Secrets:    []string{},
		},
		Operations: protocol.AdapterManifestOperations{
			"atlassian.getJiraIssue": {SideEffect: false},
			"atlassian.addJiraComment": {
				SideEffect: true,
				Approval:   approvalRequired(),
			},
			"atlassian.addWorklog": {
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

	if err := g.Approve("run-1", "ygrip"); err != nil {
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
	if err := g.Deny("run-1", "ygrip"); err != nil {
		t.Fatalf("Deny: %v", err)
	}
	if _, err := g.Call(context.Background(), "run-1", "atlassian.addJiraComment", nil); err == nil {
		t.Fatal("expected error for denied operation")
	}
	if len(fc.calls) != 0 {
		t.Fatalf("expected no adapter call, got %+v", fc.calls)
	}
}

func TestGateApprovalCoversEveryWriteInRun(t *testing.T) {
	g, fc := newTestGate(t)

	if _, err := g.RequestApproval("run-1", "atlassian.addJiraComment", protocol.ApprovalRecordRequestedBySemar); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := g.Approve("run-1", "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	if _, err := g.Call(context.Background(), "run-1", "atlassian.addWorklog", map[string]any{"timeSpentSeconds": 60}); err != nil {
		t.Fatalf("Call second operation after run approval: %v", err)
	}
	if len(fc.calls) != 1 || fc.calls[0]["op"] != "atlassian.addWorklog" {
		t.Fatalf("calls = %+v, want addWorklog", fc.calls)
	}
}

func TestGateApprovalCoversDifferentAdaptersInSameRun(t *testing.T) {
	store, err := approvals.Open(t.TempDir())
	if err != nil {
		t.Fatalf("approvals.Open: %v", err)
	}
	firstCaller := &fakeCaller{}
	secondCaller := &fakeCaller{}
	first := NewGate("atlassian", testManifest(), firstCaller, store)
	second := NewGate("another-adapter", testManifest(), secondCaller, store)

	if _, err := first.RequestApproval("run-1", "atlassian.addJiraComment", protocol.ApprovalRecordRequestedBySemar); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := first.Approve("run-1", "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if _, err := second.Call(context.Background(), "run-1", "atlassian.addWorklog", nil); err != nil {
		t.Fatalf("Call through second adapter: %v", err)
	}
	if len(secondCaller.calls) != 1 {
		t.Fatalf("second adapter calls = %+v, want one", secondCaller.calls)
	}
}

func TestGateDayScopeSharesApprovalAcrossRunIDs(t *testing.T) {
	g, fc := newTestGate(t)
	g.SetApprovalScope("day")

	if _, err := g.RequestApproval("run-1", "atlassian.addJiraComment", protocol.ApprovalRecordRequestedBySemar); err != nil {
		t.Fatalf("RequestApproval run-1: %v", err)
	}
	if err := g.Approve("run-1", "ygrip"); err != nil {
		t.Fatalf("Approve run-1: %v", err)
	}

	// A different run_id, same adapter, same day: punokawan-cy8's whole
	// point is that resuming the same task across runs should not re-prompt.
	if _, err := g.RequestApproval("run-2", "atlassian.addJiraComment", protocol.ApprovalRecordRequestedBySemar); err != nil {
		t.Fatalf("RequestApproval run-2: %v", err)
	}
	if _, err := g.Call(context.Background(), "run-2", "atlassian.addJiraComment", map[string]any{"commentBody": "hi"}); err != nil {
		t.Fatalf("Call for run-2 without a separate approval: %v", err)
	}
	if len(fc.calls) != 1 {
		t.Fatalf("calls = %+v, want one", fc.calls)
	}
}

func TestGateRunScopeIsTheDefaultAndDoesNotShareAcrossRunIDs(t *testing.T) {
	g, _ := newTestGate(t)

	if _, err := g.RequestApproval("run-1", "atlassian.addJiraComment", protocol.ApprovalRecordRequestedBySemar); err != nil {
		t.Fatalf("RequestApproval run-1: %v", err)
	}
	if err := g.Approve("run-1", "ygrip"); err != nil {
		t.Fatalf("Approve run-1: %v", err)
	}

	if _, err := g.Call(context.Background(), "run-2", "atlassian.addJiraComment", nil); err == nil {
		t.Fatal("expected run-2 to still need its own approval under the default run scope")
	}
}

func TestCallEnqueuesFailureWhenSyncQueueIsSet(t *testing.T) {
	store, err := approvals.Open(t.TempDir())
	if err != nil {
		t.Fatalf("approvals.Open: %v", err)
	}
	fc := &fakeCaller{failOps: map[string]bool{"atlassian.getJiraIssue": true}}
	g := NewGate("atlassian", testManifest(), fc, store)

	queue, err := syncqueue.Open(t.TempDir())
	if err != nil {
		t.Fatalf("syncqueue.Open: %v", err)
	}
	g.SetSyncQueue(queue)

	if _, err := g.Call(context.Background(), "run-1", "atlassian.getJiraIssue", map[string]any{"issueIdOrKey": "PAY-1"}); err == nil {
		t.Fatal("expected the simulated adapter failure to surface")
	}

	pending, err := queue.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("Pending = %+v, want exactly one queued failure", pending)
	}
	if pending[0].Adapter != "atlassian" || pending[0].Op != "atlassian.getJiraIssue" || pending[0].IssueIdOrKey != "PAY-1" {
		t.Fatalf("queued entry = %+v, want adapter/op/issue_id_or_key set from the failed call", pending[0])
	}
}

func TestCallWithoutSyncQueueJustReturnsTheError(t *testing.T) {
	g, fc := newTestGate(t)
	fc.failOps = map[string]bool{"atlassian.getJiraIssue": true}

	if _, err := g.Call(context.Background(), "run-1", "atlassian.getJiraIssue", map[string]any{"issueIdOrKey": "PAY-1"}); err == nil {
		t.Fatal("expected the simulated adapter failure to surface")
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
