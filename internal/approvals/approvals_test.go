package approvals

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestAppendListCurrentPending(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

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

func TestListOnEmptyStore(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	records, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if records != nil {
		t.Fatalf("expected nil for empty store, got %+v", records)
	}
}
