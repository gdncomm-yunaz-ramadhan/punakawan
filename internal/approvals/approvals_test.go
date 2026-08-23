package approvals

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// newTestStore opens the shared SQLite storage kernel in a temp dir and scopes
// a Store to a fixed test project id, mirroring taskstore's test setup.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, "test-project")
}

func TestAppendListCurrentPending(t *testing.T) {
	store := newTestStore(t)

	req := protocol.ApprovalRecord{
		Id:          "approval-1",
		RunId:       "run-1",
		Operation:   protocol.ApprovalRecordOperationGitPush,
		RequestedBy: protocol.ApprovalRecordRequestedByPetruk,
		Status:      protocol.ApprovalRecordStatusPending,
		CreatedAt:   time.Now().UTC(),
	}
	if err := store.Append(req); err != nil {
		t.Fatalf("Append request: %v", err)
	}

	pending, err := store.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 || pending[0].Id != "approval-1" {
		t.Fatalf("expected 1 pending record, got %+v", pending)
	}

	resolved := req
	resolvedAt := time.Now().UTC()
	resolved.Status = protocol.ApprovalRecordStatusApproved
	resolved.ResolvedAt = &resolvedAt
	if err := store.Append(resolved); err != nil {
		t.Fatalf("Append resolution: %v", err)
	}

	all, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected full history of 2 records, got %d", len(all))
	}

	current, err := store.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if len(current) != 1 {
		t.Fatalf("expected 1 distinct id, got %d", len(current))
	}
	if current["approval-1"].Status != protocol.ApprovalRecordStatusApproved {
		t.Fatalf("expected latest status approved, got %q", current["approval-1"].Status)
	}

	pendingAfter, err := store.Pending()
	if err != nil {
		t.Fatalf("Pending after resolution: %v", err)
	}
	if len(pendingAfter) != 0 {
		t.Fatalf("expected no pending records after resolution, got %+v", pendingAfter)
	}
}

func TestResolveApprovesAndDenies(t *testing.T) {
	store := newTestStore(t)
	req := protocol.ApprovalRecord{
		Id:          "approval-1",
		RunId:       "run-1",
		Operation:   protocol.ApprovalRecordOperationGitPush,
		RequestedBy: protocol.ApprovalRecordRequestedByPetruk,
		Status:      protocol.ApprovalRecordStatusPending,
		CreatedAt:   time.Now().UTC(),
	}
	if err := store.Append(req); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if err := store.Resolve("approval-1", protocol.ApprovalRecordStatusApproved, "ygrip"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	current, err := store.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	rec := current["approval-1"]
	if rec.Status != protocol.ApprovalRecordStatusApproved {
		t.Fatalf("Status = %q, want approved", rec.Status)
	}
	if rec.ApprovedBy == nil || *rec.ApprovedBy != "ygrip" {
		t.Fatalf("ApprovedBy = %v, want ygrip", rec.ApprovedBy)
	}
	if rec.ResolvedAt == nil {
		t.Fatal("ResolvedAt = nil, want set")
	}
}

func TestResolveUnknownIDFails(t *testing.T) {
	store := newTestStore(t)
	if err := store.Resolve("does-not-exist", protocol.ApprovalRecordStatusApproved, "ygrip"); err == nil {
		t.Fatal("expected an error resolving an unknown id")
	}
}

func TestResolveAlreadyResolvedFails(t *testing.T) {
	store := newTestStore(t)
	req := protocol.ApprovalRecord{
		Id:          "approval-1",
		RunId:       "run-1",
		Operation:   protocol.ApprovalRecordOperationGitPush,
		RequestedBy: protocol.ApprovalRecordRequestedByPetruk,
		Status:      protocol.ApprovalRecordStatusPending,
		CreatedAt:   time.Now().UTC(),
	}
	if err := store.Append(req); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := store.Resolve("approval-1", protocol.ApprovalRecordStatusApproved, "ygrip"); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if err := store.Resolve("approval-1", protocol.ApprovalRecordStatusDenied, "someone-else"); err == nil {
		t.Fatal("expected an error resolving an already-resolved id")
	}
}

func TestResolveRejectsAgentRoleAsApprover(t *testing.T) {
	store := newTestStore(t)
	req := protocol.ApprovalRecord{
		Id:          "approval-1",
		RunId:       "run-1",
		Operation:   protocol.ApprovalRecordOperationGitPush,
		RequestedBy: protocol.ApprovalRecordRequestedBySemar,
		Status:      protocol.ApprovalRecordStatusPending,
		CreatedAt:   time.Now().UTC(),
	}
	if err := store.Append(req); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// The requester (semar) approving its own request is the reported
	// punakawan-d3s pattern, but so is any other agent role name - none of
	// them is a human identifying themselves.
	for _, approvedBy := range []string{"semar", "Semar", " GARENG ", "petruk", "bagong"} {
		if err := store.Resolve("approval-1", protocol.ApprovalRecordStatusApproved, approvedBy); err == nil {
			t.Fatalf("Resolve with approved_by %q: expected an error, got none", approvedBy)
		}
	}

	current, err := store.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current["approval-1"].Status != protocol.ApprovalRecordStatusPending {
		t.Fatalf("Status = %q, want still pending after every self-approval attempt was rejected", current["approval-1"].Status)
	}

	if err := store.Resolve("approval-1", protocol.ApprovalRecordStatusApproved, "ygrip"); err != nil {
		t.Fatalf("Resolve with a real human name: %v", err)
	}
}

func TestListOnEmptyStore(t *testing.T) {
	store := newTestStore(t)
	records, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if records != nil {
		t.Fatalf("expected nil for empty store, got %+v", records)
	}
}

// TestProjectScopingPreventsLeakage confirms two Stores over one *storage.DB
// with distinct project ids never see each other's records on any read path.
func TestProjectScopingPreventsLeakage(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	a := New(db, "project-a")
	b := New(db, "project-b")

	rec := protocol.ApprovalRecord{
		Id:          "approval-1",
		RunId:       "run-1",
		Operation:   protocol.ApprovalRecordOperationGitPush,
		RequestedBy: protocol.ApprovalRecordRequestedByPetruk,
		Status:      protocol.ApprovalRecordStatusPending,
		CreatedAt:   time.Now().UTC(),
	}
	if err := a.Append(rec); err != nil {
		t.Fatalf("Append in A: %v", err)
	}

	// List/Current/Pending in B must not see A's record.
	if list, err := b.List(); err != nil || len(list) != 0 {
		t.Fatalf("project B List = %+v (err %v), want empty", list, err)
	}
	if cur, err := b.Current(); err != nil || len(cur) != 0 {
		t.Fatalf("project B Current = %+v (err %v), want empty", cur, err)
	}
	if pend, err := b.Pending(); err != nil || len(pend) != 0 {
		t.Fatalf("project B Pending = %+v (err %v), want empty", pend, err)
	}

	// A still sees its own record.
	if list, err := a.List(); err != nil || len(list) != 1 {
		t.Fatalf("project A List = %+v (err %v), want 1", list, err)
	}

	// B resolving A's id must fail (not found in B's scope), and must not
	// mutate A's record.
	if err := b.Resolve("approval-1", protocol.ApprovalRecordStatusApproved, "ygrip"); err == nil {
		t.Fatal("project B Resolve of A's id: expected not-found error")
	}
	curA, err := a.Current()
	if err != nil {
		t.Fatalf("Current in A: %v", err)
	}
	if curA["approval-1"].Status != protocol.ApprovalRecordStatusPending {
		t.Fatalf("project A record status = %q, want still pending", curA["approval-1"].Status)
	}
}
