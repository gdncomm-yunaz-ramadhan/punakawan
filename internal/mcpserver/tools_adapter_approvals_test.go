package mcpserver

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func pendingAdapterApproval(t *testing.T) (*approvals.Store, protocol.ApprovalRecord) {
	t.Helper()
	store, err := approvals.Open(t.TempDir())
	if err != nil {
		t.Fatalf("approvals.Open: %v", err)
	}
	rec := protocol.ApprovalRecord{
		Id:          "approval-adapter-run-run-1",
		RunId:       "run-1",
		Operation:   protocol.ApprovalRecordOperationExternalWrite,
		RequestedBy: protocol.ApprovalRecordRequestedBySemar,
		Status:      protocol.ApprovalRecordStatusPending,
		CreatedAt:   time.Now().UTC(),
	}
	if err := store.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}
	return store, rec
}

func TestRespondToAdapterApprovalRecordsExplicitApprove(t *testing.T) {
	store, rec := pendingAdapterApproval(t)
	out, err := respondToAdapterApproval(store, RespondToAdapterApprovalInput{
		ApprovalId:  rec.Id,
		Decision:    "approve",
		ConfirmedBy: "yunaz",
	})
	if err != nil {
		t.Fatalf("respondToAdapterApproval: %v", err)
	}
	if out.Status != protocol.ApprovalRecordStatusApproved || out.RunId != rec.RunId {
		t.Fatalf("output = %+v", out)
	}
	current, err := store.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current[rec.Id].Status != protocol.ApprovalRecordStatusApproved {
		t.Fatalf("stored status = %q", current[rec.Id].Status)
	}
}

func TestRespondToAdapterApprovalRecordsExplicitDeny(t *testing.T) {
	store, rec := pendingAdapterApproval(t)
	out, err := respondToAdapterApproval(store, RespondToAdapterApprovalInput{
		ApprovalId:  rec.Id,
		Decision:    "deny",
		ConfirmedBy: "yunaz",
	})
	if err != nil {
		t.Fatalf("respondToAdapterApproval: %v", err)
	}
	if out.Status != protocol.ApprovalRecordStatusDenied {
		t.Fatalf("output = %+v", out)
	}
}

func TestRespondToAdapterApprovalRejectsNonAdapterAndInferredChoices(t *testing.T) {
	store, rec := pendingAdapterApproval(t)
	for _, in := range []RespondToAdapterApprovalInput{
		{ApprovalId: "approval-worktree-1", Decision: "approve", ConfirmedBy: "yunaz"},
		{ApprovalId: rec.Id, Decision: "maybe", ConfirmedBy: "yunaz"},
		{ApprovalId: rec.Id, Decision: "approve"},
	} {
		if _, err := respondToAdapterApproval(store, in); err == nil {
			t.Fatalf("expected rejection for %+v", in)
		}
	}
}
