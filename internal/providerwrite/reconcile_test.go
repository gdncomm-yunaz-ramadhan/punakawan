package providerwrite

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/outbox"
)

type fakeJiraIssue struct {
	status   string
	subtasks []map[string]any
}

// fakeIssueProvider answers atlassian.getJiraIssue with a fixed
// status/subtasks payload, so ReconcileJiraTransition and
// ReconcileJiraCreateSubtask can be exercised without a real subprocess.
type fakeIssueProvider struct{ issue fakeJiraIssue }

func (f fakeIssueProvider) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	args, _ := params.(map[string]any)
	op, _ := args["op"].(string)
	if op != "atlassian.getJiraIssue" {
		return nil, fmt.Errorf("fakeIssueProvider: unhandled op %q", op)
	}
	raw, _ := json.Marshal(map[string]any{
		"normalized": map[string]any{"status": f.issue.status, "subtasks": f.issue.subtasks},
	})
	return raw, nil
}

func TestReconcileJiraTransitionAppliedWhenStatusAlreadyMatches(t *testing.T) {
	gate := adapters.NewGate("atlassian", atlassianManifest(), fakeIssueProvider{issue: fakeJiraIssue{status: "Done"}})
	payload, _ := json.Marshal(map[string]any{"target_status": "Done"})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "ABC-1", PayloadJSON: string(payload)}

	result, err := ReconcileJiraTransition(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileJiraTransition: %v", err)
	}
	if result.State != ReconcileApplied {
		t.Fatalf("state = %v, want ReconcileApplied", result.State)
	}
}

func TestReconcileJiraTransitionNotAppliedWhenStatusDiffers(t *testing.T) {
	gate := adapters.NewGate("atlassian", atlassianManifest(), fakeIssueProvider{issue: fakeJiraIssue{status: "In Progress"}})
	payload, _ := json.Marshal(map[string]any{"target_status": "Done"})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "ABC-1", PayloadJSON: string(payload)}

	result, err := ReconcileJiraTransition(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileJiraTransition: %v", err)
	}
	if result.State != ReconcileNotApplied {
		t.Fatalf("state = %v, want ReconcileNotApplied", result.State)
	}
}

func TestReconcileJiraCreateSubtaskMatchesByNormalizedSummary(t *testing.T) {
	gate := adapters.NewGate("atlassian", atlassianManifest(), fakeIssueProvider{issue: fakeJiraIssue{
		subtasks: []map[string]any{{"key": "ABC-2", "summary": "  Fix   the Bug "}},
	}})
	payload, _ := json.Marshal(map[string]any{"candidates": []map[string]any{{"summary": "fix the bug"}}})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "ABC-1", PayloadJSON: string(payload)}

	result, err := ReconcileJiraCreateSubtask(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileJiraCreateSubtask: %v", err)
	}
	if result.State != ReconcileApplied || result.ExternalID != "ABC-2" {
		t.Fatalf("result = %+v, want ReconcileApplied with ABC-2", result)
	}
}

func TestReconcileJiraCreateSubtaskNotAppliedWhenNoMatch(t *testing.T) {
	gate := adapters.NewGate("atlassian", atlassianManifest(), fakeIssueProvider{issue: fakeJiraIssue{
		subtasks: []map[string]any{{"key": "ABC-2", "summary": "something else"}},
	}})
	payload, _ := json.Marshal(map[string]any{"candidates": []map[string]any{{"summary": "fix the bug"}}})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "ABC-1", PayloadJSON: string(payload)}

	result, err := ReconcileJiraCreateSubtask(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileJiraCreateSubtask: %v", err)
	}
	if result.State != ReconcileNotApplied {
		t.Fatalf("state = %v, want ReconcileNotApplied", result.State)
	}
}

type fakeWorklogProvider struct{ worklogs []map[string]any }

func (f fakeWorklogProvider) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	args, _ := params.(map[string]any)
	op, _ := args["op"].(string)
	if op != "atlassian.listJiraWorklogs" {
		return nil, fmt.Errorf("fakeWorklogProvider: unhandled op %q", op)
	}
	raw, _ := json.Marshal(map[string]any{"worklogs": f.worklogs})
	return raw, nil
}

func TestReconcileJiraWorklogAppliedWhenMarkerFound(t *testing.T) {
	gate := adapters.NewGate("atlassian", atlassianManifest(), fakeWorklogProvider{worklogs: []map[string]any{
		{"id": "wl-1", "comment": "Implemented retry\n\n[" + jiraWorklogMarker("intent-1") + "]"},
	}})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "ABC-1"}

	result, err := ReconcileJiraWorklog(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileJiraWorklog: %v", err)
	}
	if result.State != ReconcileApplied || result.ExternalID != "wl-1" {
		t.Fatalf("result = %+v, want ReconcileApplied with wl-1", result)
	}
}

func TestReconcileJiraWorklogNotAppliedWhenNoMarkerFound(t *testing.T) {
	gate := adapters.NewGate("atlassian", atlassianManifest(), fakeWorklogProvider{worklogs: []map[string]any{
		{"id": "wl-1", "comment": "unrelated worklog"},
	}})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "ABC-1"}

	result, err := ReconcileJiraWorklog(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileJiraWorklog: %v", err)
	}
	if result.State != ReconcileNotApplied {
		t.Fatalf("state = %v, want ReconcileNotApplied", result.State)
	}
}

// TestWorkerFallsBackToUnknownForUnregisteredReconciliation exercises the
// one operation with no registered reconciler (atlassian.editJiraIssue - a
// field edit's "applied" state cannot be distinguished from "not applied"
// by re-reading the issue, since re-reading only shows a field's current
// value, never whether this exact intent is what set it): the intent must
// stay reconciling with a diagnostic rather than ever being replayed.
func TestWorkerFallsBackToUnknownForUnregisteredReconciliation(t *testing.T) {
	remote := &fakeProvider{}
	store, worker := newWorkerHarness(t, remote)
	ctx := context.Background()

	payload, _ := json.Marshal(map[string]any{"fields": map[string]any{"summary": "New summary"}})
	intent := outbox.Intent{
		OrchestrationID: "orch-1", AdapterID: "atlassian", Operation: "atlassian.editJiraIssue",
		TargetKey: "ABC-1", PayloadJSON: string(payload), OperationFingerprint: "intent-edit-1",
	}
	enqueued, err := store.Enqueue(ctx, intent)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, err := store.ClaimByID(ctx, enqueued.ID, "worker-1", time.Now(), time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("ClaimByID: claimed=%v err=%v", claimed, err)
	}
	if _, err := store.MarkAmbiguous(ctx, claimed.ID, "worker-1", "", "simulated ambiguous attempt"); err != nil {
		t.Fatalf("MarkAmbiguous: %v", err)
	}

	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got, err := store.GetByFingerprint(ctx, "intent-edit-1")
	if err != nil {
		t.Fatalf("GetByFingerprint: %v", err)
	}
	if got.Status != outbox.StatusReconciling {
		t.Fatalf("status = %q, want reconciling (never replayed without a reconciler)", got.Status)
	}
}
