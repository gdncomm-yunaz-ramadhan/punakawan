package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/syncqueue"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeCaller records every "execute" call it receives instead of talking to
// a real subprocess, so Gate's call behavior can be tested in isolation.
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
			"atlassian.getJiraIssue":   {SideEffect: false},
			"atlassian.addJiraComment": {SideEffect: true},
			"atlassian.addWorklog":     {SideEffect: true},
		},
	}
}

func newTestGate(t *testing.T) (*Gate, *fakeCaller) {
	t.Helper()
	fc := &fakeCaller{}
	return NewGate("atlassian", testManifest(), fc), fc
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

func TestGateAllowsSideEffectingOperationDirectly(t *testing.T) {
	// side_effect no longer implies an approval gate: Call always proceeds
	// straight to the adapter.
	g, fc := newTestGate(t)

	if _, err := g.Call(context.Background(), "run-1", "atlassian.addJiraComment", map[string]any{"commentBody": "hi"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(fc.calls) != 1 || fc.calls[0]["op"] != "atlassian.addJiraComment" {
		t.Fatalf("calls = %+v", fc.calls)
	}
}

func TestCallEnqueuesFailureWhenSyncQueueIsSet(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	fc := &fakeCaller{failOps: map[string]bool{"atlassian.getJiraIssue": true}}
	g := NewGate("atlassian", testManifest(), fc)

	queue := syncqueue.New(db, "test-project")
	g.SetSyncQueue(func() (*syncqueue.Queue, error) { return queue, nil })

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
