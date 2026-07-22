package mcpserver

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/syncqueue"
)

func TestListJiraSyncQueueHandlerFiltersByRunAndDefaultsToPending(t *testing.T) {
	a := newTestApp(t)

	if _, err := a.SyncQueue.Enqueue(syncqueue.Entry{Id: "sync-1", RunId: "run-1", Adapter: "atlassian", Op: "atlassian.addWorklog", Error: "timeout"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := a.SyncQueue.Enqueue(syncqueue.Entry{Id: "sync-2", RunId: "run-2", Adapter: "atlassian", Op: "atlassian.addWorklog", Error: "timeout"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := a.SyncQueue.Resolve("sync-2", syncqueue.StatusResolved); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	_, out, err := listJiraSyncQueueHandler(a)(context.Background(), nil, ListJiraSyncQueueInput{})
	if err != nil {
		t.Fatalf("listJiraSyncQueueHandler: %v", err)
	}
	if len(out.Entries) != 1 || out.Entries[0].Id != "sync-1" {
		t.Fatalf("Entries = %+v, want only the still-pending sync-1", out.Entries)
	}

	_, filtered, err := listJiraSyncQueueHandler(a)(context.Background(), nil, ListJiraSyncQueueInput{RunId: "run-2", IncludeResolved: true})
	if err != nil {
		t.Fatalf("listJiraSyncQueueHandler: %v", err)
	}
	if len(filtered.Entries) != 1 || filtered.Entries[0].Id != "sync-2" {
		t.Fatalf("Entries = %+v, want only sync-2 for run_id=run-2 with resolved included", filtered.Entries)
	}
}

func TestRetryJiraSyncEntryHandlerRejectsUnknownEntry(t *testing.T) {
	a := newTestApp(t)
	if _, _, err := retryJiraSyncEntryHandler(a)(context.Background(), nil, RetryJiraSyncEntryInput{EntryId: "does-not-exist"}); err == nil {
		t.Fatal("expected an error retrying an entry that was never enqueued")
	}
}

func TestRetryJiraSyncEntryHandlerRejectsAlreadyResolvedEntry(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.SyncQueue.Enqueue(syncqueue.Entry{Id: "sync-1", RunId: "run-1", Adapter: "atlassian", Op: "atlassian.addWorklog", Error: "timeout"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := a.SyncQueue.Resolve("sync-1", syncqueue.StatusResolved); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if _, _, err := retryJiraSyncEntryHandler(a)(context.Background(), nil, RetryJiraSyncEntryInput{EntryId: "sync-1"}); err == nil {
		t.Fatal("expected an error retrying an already-resolved entry")
	}
}

func TestRetryJiraSyncEntryHandlerResolvesOnSuccess(t *testing.T) {
	if _, err := os.Stat(prototypeAdapterTestPath); err != nil {
		t.Skipf("prototype adapter not built (%s): %v; run `pnpm --filter @punakawan/adapter-sdk build` first", prototypeAdapterTestPath, err)
	}

	a := newTestApp(t)
	registry := adapters.NewRegistry(map[string]adapters.AdapterSpec{
		"prototype": {Command: "node", Args: []string{prototypeAdapterTestPath}},
	}, a.Approvals)
	registry.SetSyncQueue(a.SyncQueue)
	a.AdapterRegistry = registry

	if _, err := a.SyncQueue.Enqueue(syncqueue.Entry{
		Id: "sync-1", RunId: "run-1", Adapter: "prototype", Op: "sleep",
		Params: map[string]any{"ms": 0}, Error: "simulated prior failure",
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, out, err := retryJiraSyncEntryHandler(a)(ctx, nil, RetryJiraSyncEntryInput{EntryId: "sync-1"})
	if err != nil {
		t.Fatalf("retryJiraSyncEntryHandler: %v", err)
	}
	if !out.Resolved {
		t.Fatal("Resolved = false, want true")
	}

	current, err := a.SyncQueue.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current["sync-1"].Status != syncqueue.StatusResolved {
		t.Fatalf("Current[sync-1].Status = %s, want resolved", current["sync-1"].Status)
	}
}

// prototypeAdapterTestPath mirrors internal/adapters/registry_test.go's
// prototypeAdapterPath - both packages are two directories below the repo
// root, so the relative path is identical.
const prototypeAdapterTestPath = "../../packages/adapter-sdk/dist/prototypeAdapter.js"
