package syncqueue

import "testing"

func TestEnqueueThenPendingReturnsIt(t *testing.T) {
	q, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	entry, err := q.Enqueue(Entry{Id: "sync-1", RunId: "run-1", Adapter: "atlassian", Op: "atlassian.addWorklog", IssueIdOrKey: "PAY-1", Error: "timeout"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if entry.Status != StatusPending || entry.Attempts != 1 {
		t.Fatalf("Enqueue result = %+v, want status=pending attempts=1", entry)
	}

	pending, err := q.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 || pending[0].Id != "sync-1" {
		t.Fatalf("Pending = %+v, want [sync-1]", pending)
	}
}

func TestEnqueueSameIdAgainIncrementsAttempts(t *testing.T) {
	q, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := q.Enqueue(Entry{Id: "sync-1", RunId: "run-1", Adapter: "atlassian", Op: "atlassian.addWorklog", IssueIdOrKey: "PAY-1", Error: "timeout"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	second, err := q.Enqueue(Entry{Id: "sync-1", RunId: "run-1", Adapter: "atlassian", Op: "atlassian.addWorklog", IssueIdOrKey: "PAY-1", Error: "timeout again"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if second.Attempts != 2 {
		t.Fatalf("second Enqueue Attempts = %d, want 2", second.Attempts)
	}

	current, err := q.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if len(current) != 1 {
		t.Fatalf("Current = %+v, want exactly one entry (folded by id)", current)
	}
}

func TestEnqueueDetectsConflictAgainstAnotherPendingEntry(t *testing.T) {
	q, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := q.Enqueue(Entry{Id: "sync-1", RunId: "run-1", Adapter: "atlassian", Op: "atlassian.transitionJiraIssue", IssueIdOrKey: "PAY-1", Error: "timeout"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	second, err := q.Enqueue(Entry{Id: "sync-2", RunId: "run-2", Adapter: "atlassian", Op: "atlassian.transitionJiraIssue", IssueIdOrKey: "PAY-1", Error: "timeout"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if second.ConflictsWith != "sync-1" {
		t.Fatalf("second.ConflictsWith = %q, want sync-1", second.ConflictsWith)
	}
}

func TestResolveMarksEntryResolvedAndDropsFromPending(t *testing.T) {
	q, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := q.Enqueue(Entry{Id: "sync-1", RunId: "run-1", Adapter: "atlassian", Op: "atlassian.addWorklog", Error: "timeout"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := q.Resolve("sync-1", StatusResolved); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	pending, err := q.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("Pending = %+v, want none after resolving the only entry", pending)
	}

	current, err := q.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current["sync-1"].Status != StatusResolved {
		t.Fatalf("Current[sync-1].Status = %s, want resolved", current["sync-1"].Status)
	}
}

func TestResolveRejectsUnknownEntry(t *testing.T) {
	q, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := q.Resolve("does-not-exist", StatusResolved); err == nil {
		t.Fatal("expected an error resolving an entry that was never enqueued")
	}
}

func TestResolveRejectsAlreadyResolvedEntry(t *testing.T) {
	q, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := q.Enqueue(Entry{Id: "sync-1", RunId: "run-1", Adapter: "atlassian", Op: "atlassian.addWorklog", Error: "timeout"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := q.Resolve("sync-1", StatusResolved); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := q.Resolve("sync-1", StatusResolved); err == nil {
		t.Fatal("expected an error resolving an already-resolved entry")
	}
}

func TestPendingOnEmptyQueue(t *testing.T) {
	q, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	pending, err := q.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if pending != nil {
		t.Fatalf("expected nil for an empty queue, got %+v", pending)
	}
}
