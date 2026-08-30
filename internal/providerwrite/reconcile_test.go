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
// status/subtasks payload, so reconcileJiraTransition and
// reconcileJiraCreateSubtask can be exercised without a real subprocess.
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

	result, err := reconcileJiraTransition(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("reconcileJiraTransition: %v", err)
	}
	if result.State != ReconcileApplied {
		t.Fatalf("state = %v, want ReconcileApplied", result.State)
	}
}

func TestReconcileJiraTransitionNotAppliedWhenStatusDiffers(t *testing.T) {
	gate := adapters.NewGate("atlassian", atlassianManifest(), fakeIssueProvider{issue: fakeJiraIssue{status: "In Progress"}})
	payload, _ := json.Marshal(map[string]any{"target_status": "Done"})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "ABC-1", PayloadJSON: string(payload)}

	result, err := reconcileJiraTransition(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("reconcileJiraTransition: %v", err)
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

	result, err := reconcileJiraCreateSubtask(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("reconcileJiraCreateSubtask: %v", err)
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

	result, err := reconcileJiraCreateSubtask(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("reconcileJiraCreateSubtask: %v", err)
	}
	if result.State != ReconcileNotApplied {
		t.Fatalf("state = %v, want ReconcileNotApplied", result.State)
	}
}

// TestWorkerFallsBackToUnknownForUnregisteredReconciliation exercises an
// operation with no registered reconciler (e.g. jira.worklog, or any GitHub
// write): the intent must stay reconciling with a diagnostic rather than
// ever being replayed.
func TestWorkerFallsBackToUnknownForUnregisteredReconciliation(t *testing.T) {
	remote := &fakeProvider{}
	store, worker := newWorkerHarness(t, remote)
	ctx := context.Background()

	payload, _ := json.Marshal(map[string]any{"time_spent_seconds": 60})
	intent := outbox.Intent{
		OrchestrationID: "orch-1", AdapterID: "atlassian", Operation: "atlassian.addWorklog",
		TargetKey: "ABC-1", PayloadJSON: string(payload), OperationFingerprint: "intent-worklog-1",
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
	got, err := store.GetByFingerprint(ctx, "intent-worklog-1")
	if err != nil {
		t.Fatalf("GetByFingerprint: %v", err)
	}
	if got.Status != outbox.StatusReconciling {
		t.Fatalf("status = %q, want reconciling (never replayed without a reconciler)", got.Status)
	}
}
