package mcpserver

import (
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestListPendingApprovals(t *testing.T) {
	store, rec := pendingAdapterApproval(t)

	// Unfiltered: the pending approval is returned (durable re-check).
	out, err := listPendingApprovals(store, ListPendingApprovalsInput{})
	if err != nil {
		t.Fatalf("listPendingApprovals: %v", err)
	}
	if out.Count != 1 || len(out.Pending) != 1 {
		t.Fatalf("want 1 pending, got %d", out.Count)
	}
	if out.Pending[0].ApprovalId != rec.Id || out.Pending[0].RunId != rec.RunId {
		t.Fatalf("unexpected item: %+v", out.Pending[0])
	}

	// Filter by matching run id.
	if out, _ := listPendingApprovals(store, ListPendingApprovalsInput{RunId: rec.RunId}); out.Count != 1 {
		t.Fatalf("run filter (match): want 1, got %d", out.Count)
	}
	// Filter by non-matching run id.
	if out, _ := listPendingApprovals(store, ListPendingApprovalsInput{RunId: "other-run"}); out.Count != 0 {
		t.Fatalf("run filter (miss): want 0, got %d", out.Count)
	}
}

func TestListPendingApprovalsEmptyAfterResolve(t *testing.T) {
	store, rec := pendingAdapterApproval(t)
	if err := store.Resolve(rec.Id, protocol.ApprovalRecordStatusApproved, "yunaz"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	out, err := listPendingApprovals(store, ListPendingApprovalsInput{})
	if err != nil {
		t.Fatalf("listPendingApprovals: %v", err)
	}
	if out.Count != 0 {
		t.Fatalf("want 0 pending after resolve, got %d", out.Count)
	}
}
