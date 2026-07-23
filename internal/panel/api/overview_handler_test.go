package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/panel"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

type fakeWorkspaceReader struct {
	summaries []contract.WorkspaceSummary
}

func (f fakeWorkspaceReader) List(ctx context.Context) ([]contract.WorkspaceSummary, error) {
	return f.summaries, nil
}
func (f fakeWorkspaceReader) Get(ctx context.Context, id string) (contract.WorkspaceDetail, error) {
	return contract.WorkspaceDetail{}, errors.New("not implemented")
}

type fakeSessionReader struct {
	sessions []protocol.PanelSessionSummary
}

func (f fakeSessionReader) List(ctx context.Context, workspaceID string, filter contract.SessionFilter) ([]protocol.PanelSessionSummary, error) {
	return f.sessions, nil
}
func (f fakeSessionReader) Get(ctx context.Context, workspaceID, sessionID string) (contract.SessionDetail, error) {
	return contract.SessionDetail{}, errors.New("not implemented")
}

type fakeApprovalReader struct {
	pending []protocol.ApprovalRecord
}

func (f fakeApprovalReader) List(ctx context.Context, workspaceID string, filter contract.ApprovalFilter) ([]protocol.ApprovalRecord, error) {
	if filter.Status == "pending" {
		return f.pending, nil
	}
	return nil, nil
}

func TestOverviewHandlerOrdersNeedsAttentionByPriority(t *testing.T) {
	now := time.Now().UTC()
	readers := panel.Readers{
		Workspace: fakeWorkspaceReader{summaries: []contract.WorkspaceSummary{
			{ID: "ws-a", BlockedTaskCount: 2, Availability: protocol.PanelSourceHealthAvailabilityAvailable},
			{ID: "ws-b", Availability: protocol.PanelSourceHealthAvailabilityUnavailable},
		}},
		Session: fakeSessionReader{sessions: []protocol.PanelSessionSummary{
			{Id: "run-failed", WorkspaceId: "ws-a", Status: "failed", UpdatedAt: now},
			{Id: "run-stale", WorkspaceId: "ws-a", Status: "executing", UpdatedAt: now.Add(-time.Hour)},
			{Id: "run-active", WorkspaceId: "ws-a", Status: "executing", UpdatedAt: now},
		}},
		Approval: fakeApprovalReader{pending: []protocol.ApprovalRecord{
			{Id: "appr-1", RunId: "run-active", Operation: protocol.ApprovalRecordOperationGitPush, RequestedBy: protocol.ApprovalRecordRequestedByPetruk, Status: protocol.ApprovalRecordStatusPending, CreatedAt: now},
		}},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	rec := httptest.NewRecorder()
	OverviewHandler(readers, "ws-a")(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var out Overview
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}

	wantOrder := []NeedsAttentionKind{
		NeedsAttentionFailedSession,
		NeedsAttentionPendingApproval,
		NeedsAttentionBlockedTasks,
		NeedsAttentionUnavailableWorkspace,
		NeedsAttentionStaleSession,
	}
	if len(out.NeedsAttention) != len(wantOrder) {
		t.Fatalf("NeedsAttention = %+v, want %d items", out.NeedsAttention, len(wantOrder))
	}
	for i, kind := range wantOrder {
		if out.NeedsAttention[i].Kind != kind {
			t.Fatalf("NeedsAttention[%d].Kind = %q, want %q (full: %+v)", i, out.NeedsAttention[i].Kind, kind, out.NeedsAttention)
		}
	}

	if out.BlockedTasks != 2 {
		t.Fatalf("BlockedTasks = %d, want 2", out.BlockedTasks)
	}
	if out.AvailableWorkspaces != 1 {
		t.Fatalf("AvailableWorkspaces = %d, want 1", out.AvailableWorkspaces)
	}
	if len(out.ActiveSessions) != 2 {
		t.Fatalf("ActiveSessions = %+v, want 2 (run-stale and run-active)", out.ActiveSessions)
	}
}
