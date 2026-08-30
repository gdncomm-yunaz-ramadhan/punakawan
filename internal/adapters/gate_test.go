package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeCaller records every "execute" call it receives instead of talking to
// a real subprocess, so Gate's call behavior can be tested in isolation.
// failOps, if set, makes Call return an error instead of a canned success
// for any op named in it.
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

// TestRawAdapterWriteIsRejected is the exact regression this Gate change
// exists for: a raw call naming a side-effecting operation must fail before
// the adapter process is ever invoked, so every write is forced through the
// durable outbox instead.
func TestRawAdapterWriteIsRejected(t *testing.T) {
	g, fc := newTestGate(t)

	_, err := g.Call(context.Background(), "run-1", "atlassian.addJiraComment", map[string]any{"commentBody": "hi"})
	if err == nil {
		t.Fatal("expected a side-effecting raw call to be rejected")
	}
	if got := err.Error(); !strings.Contains(got, "side-effecting operation") || !strings.Contains(got, "must be enqueued through a domain service") {
		t.Fatalf("error = %q, want it to explain the operation must be enqueued", got)
	}
	if len(fc.calls) != 0 {
		t.Fatalf("expected the adapter process to never be invoked, got %d calls", len(fc.calls))
	}
}

func TestCallRejectsUndeclaredOperation(t *testing.T) {
	g, fc := newTestGate(t)
	if _, err := g.Call(context.Background(), "run-1", "atlassian.doesNotExist", nil); err == nil {
		t.Fatal("expected an undeclared operation to be rejected")
	}
	if len(fc.calls) != 0 {
		t.Fatalf("expected the adapter process to never be invoked, got %d calls", len(fc.calls))
	}
}

func TestExecuteWriteInvokesASideEffectingOperationDirectly(t *testing.T) {
	// ExecuteWrite is the one seam internal/providerwrite's worker uses to
	// perform a write it already claimed from the durable outbox - this is
	// the only place a side-effecting operation may still reach the adapter
	// process directly.
	g, fc := newTestGate(t)
	if _, err := g.ExecuteWrite(context.Background(), "run-1", "atlassian.addJiraComment", map[string]any{"commentBody": "hi"}); err != nil {
		t.Fatalf("ExecuteWrite: %v", err)
	}
	if len(fc.calls) != 1 || fc.calls[0]["op"] != "atlassian.addJiraComment" {
		t.Fatalf("calls = %+v", fc.calls)
	}
}

func TestExecuteWriteRejectsANonSideEffectingOperation(t *testing.T) {
	g, _ := newTestGate(t)
	if _, err := g.ExecuteWrite(context.Background(), "run-1", "atlassian.getJiraIssue", nil); err == nil {
		t.Fatal("expected ExecuteWrite to reject a read-only operation")
	}
}

func TestCallReturnsTheAdapterErrorOnFailure(t *testing.T) {
	g, fc := newTestGate(t)
	fc.failOps = map[string]bool{"atlassian.getJiraIssue": true}

	if _, err := g.Call(context.Background(), "run-1", "atlassian.getJiraIssue", map[string]any{"issueIdOrKey": "PAY-1"}); err == nil {
		t.Fatal("expected the simulated adapter failure to surface")
	}
}
