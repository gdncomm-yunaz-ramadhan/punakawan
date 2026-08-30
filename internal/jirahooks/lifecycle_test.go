package jirahooks

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/storage"
)

func TestFormatTransitionCatalogIncludesEveryAvailableID(t *testing.T) {
	catalog := formatTransitionCatalog([]adapters.JiraTransition{
		{ID: "41", Name: "Submit", ToStatusName: "In Review"},
		{ID: "52", Name: "Close", ToStatusName: "Done"},
	})
	const want = "Available transitions:\n- Submit (41) -> In Review\n- Close (52) -> Done"
	if catalog != want {
		t.Fatalf("catalog = %q, want %q", catalog, want)
	}
}

func TestRetryWorkLogSyncReplaysExistingLedgerEntry(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := delivery.NewStore(db)
	ob := outbox.New(db)
	resolved, err := store.ResolveJiraDelivery(ctx, "resolve", "TRF-19272", delivery.ResolveJiraDeliveryOptions{})
	if err != nil {
		t.Fatalf("ResolveJiraDelivery: %v", err)
	}
	worklogID := "worklog-retry"
	if _, err := db.Reader().ExecContext(ctx, `
		INSERT INTO delivery_worklogs
			(id, orchestration_id, case_id, execution_id, lane_id, parent_task_id, session_id, jira_issue_key, started_at, duration_seconds, summary, created_at)
		VALUES (?, ?, ?, ?, 'lane-1', '', 'session-1', 'TRF-19272', ?, 900, 'Recovery', ?)
	`, worklogID, resolved.Execution.OrchestrationID, resolved.Case.ID, resolved.Execution.ID, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert worklog: %v", err)
	}
	caller := &fakeAdapterCaller{responses: map[string]string{"atlassian.addWorklog": `{"ok":true,"worklogId":"jira-worklog-1"}`}, failOps: map[string]bool{}}
	gate := adapters.NewGate("atlassian", testManifest(), caller)
	lifecycle := NewLifecycle(store, &fakeGateResolver{gate: gate}, ob)

	entry, err := lifecycle.RetryWorkLogSync(ctx, resolved.Execution.OrchestrationID, worklogID)
	if err != nil {
		t.Fatalf("RetryWorkLogSync: %v", err)
	}
	if entry.SyncStatus != "synced" || entry.JiraWorklogID != "jira-worklog-1" {
		t.Fatalf("entry = %+v, want synced Jira worklog", entry)
	}
	if len(caller.calls) != 1 || caller.calls[0]["op"] != "atlassian.addWorklog" {
		t.Fatalf("calls = %+v, want one addWorklog", caller.calls)
	}
	if caller.calls[0]["timeSpentSeconds"] != 900 {
		t.Fatalf("worklog params = %+v", caller.calls[0])
	}
	if _, err := store.GetWorkLog(ctx, resolved.Execution.OrchestrationID, worklogID); err != nil {
		t.Fatalf("existing worklog was not retained: %v", err)
	}
	worklogs, err := store.ListWorkLogs(ctx, resolved.Execution.OrchestrationID)
	if err != nil {
		t.Fatalf("ListWorkLogs: %v", err)
	}
	if len(worklogs) != 1 {
		t.Fatalf("worklogs = %+v, want exactly one retained ledger entry", worklogs)
	}

	// A second retry of an already-synced entry must not call Jira again.
	again, err := lifecycle.RetryWorkLogSync(ctx, resolved.Execution.OrchestrationID, worklogID)
	if err != nil {
		t.Fatalf("RetryWorkLogSync (already synced): %v", err)
	}
	if again.SyncStatus != "synced" {
		t.Fatalf("entry = %+v, want still synced", again)
	}
	if len(caller.calls) != 1 {
		t.Fatalf("calls after redundant retry = %+v, want still exactly one", caller.calls)
	}
}
