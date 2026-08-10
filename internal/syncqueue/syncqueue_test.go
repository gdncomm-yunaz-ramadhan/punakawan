package syncqueue

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/storage"
)

// newTestQueue opens the shared SQLite storage kernel in a temp dir and scopes
// a Queue to a fixed test project id, mirroring approvals' test setup.
func newTestQueue(t *testing.T) *Queue {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, "test-project")
}

func TestEnqueueThenPendingReturnsIt(t *testing.T) {
	q := newTestQueue(t)

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
	q := newTestQueue(t)

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
	q := newTestQueue(t)

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
	q := newTestQueue(t)
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
	q := newTestQueue(t)
	if err := q.Resolve("does-not-exist", StatusResolved); err == nil {
		t.Fatal("expected an error resolving an entry that was never enqueued")
	}
}

func TestResolveRejectsAlreadyResolvedEntry(t *testing.T) {
	q := newTestQueue(t)
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

func TestResolveRejectsInvalidStatus(t *testing.T) {
	q := newTestQueue(t)
	if _, err := q.Enqueue(Entry{Id: "sync-1", RunId: "run-1", Adapter: "atlassian", Op: "atlassian.addWorklog", Error: "timeout"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := q.Resolve("sync-1", StatusPending); err == nil {
		t.Fatal("expected an error resolving to a status other than resolved/abandoned")
	}
}

func TestPendingOnEmptyQueue(t *testing.T) {
	q := newTestQueue(t)
	pending, err := q.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if pending != nil {
		t.Fatalf("expected nil for an empty queue, got %+v", pending)
	}
}

func TestListOnEmptyQueue(t *testing.T) {
	q := newTestQueue(t)
	entries, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if entries != nil {
		t.Fatalf("expected nil for an empty queue, got %+v", entries)
	}
}

// TestProjectScopingPreventsLeakage confirms two Queues over one *storage.DB
// with distinct project ids never see each other's entries on any read path,
// and - critically - that an entry in project A is never picked up as a
// ConflictsWith match for project B's Enqueue.
func TestProjectScopingPreventsLeakage(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	a := New(db, "project-a")
	b := New(db, "project-b")

	if _, err := a.Enqueue(Entry{Id: "sync-1", RunId: "run-1", Adapter: "atlassian", Op: "atlassian.transitionJiraIssue", IssueIdOrKey: "PAY-1", Error: "timeout"}); err != nil {
		t.Fatalf("Enqueue in A: %v", err)
	}

	// List/Current/Pending in B must not see A's entry.
	if list, err := b.List(); err != nil || len(list) != 0 {
		t.Fatalf("project B List = %+v (err %v), want empty", list, err)
	}
	if cur, err := b.Current(); err != nil || len(cur) != 0 {
		t.Fatalf("project B Current = %+v (err %v), want empty", cur, err)
	}
	if pend, err := b.Pending(); err != nil || len(pend) != 0 {
		t.Fatalf("project B Pending = %+v (err %v), want empty", pend, err)
	}

	// B enqueuing a write targeting the same (adapter, op, issue) as A's
	// pending entry must NOT flag a conflict - A's entry lives in a different
	// project scope and must be invisible to B's conflict scan.
	bEntry, err := b.Enqueue(Entry{Id: "sync-2", RunId: "run-2", Adapter: "atlassian", Op: "atlassian.transitionJiraIssue", IssueIdOrKey: "PAY-1", Error: "timeout"})
	if err != nil {
		t.Fatalf("Enqueue in B: %v", err)
	}
	if bEntry.ConflictsWith != "" {
		t.Fatalf("project B entry ConflictsWith = %q, want empty (A's entry is in another project scope)", bEntry.ConflictsWith)
	}

	// A still sees only its own entry.
	if list, err := a.List(); err != nil || len(list) != 1 {
		t.Fatalf("project A List = %+v (err %v), want 1", list, err)
	}

	// B resolving A's id must fail (not found in B's scope), and must not
	// mutate A's entry.
	if err := b.Resolve("sync-1", StatusResolved); err == nil {
		t.Fatal("project B Resolve of A's id: expected not-found error")
	}
	curA, err := a.Current()
	if err != nil {
		t.Fatalf("Current in A: %v", err)
	}
	if curA["sync-1"].Status != StatusPending {
		t.Fatalf("project A entry status = %q, want still pending", curA["sync-1"].Status)
	}
}
