package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// filteringApprovalReader is a fuller stand-in than overview_handler_test.go's
// fakeApprovalReader (which only understands status=pending): it filters
// by any requested status, matching what ApprovalSource actually does.
type filteringApprovalReader struct {
	records []protocol.ApprovalRecord
}

func (f filteringApprovalReader) List(ctx context.Context, workspaceID string, filter contract.ApprovalFilter) ([]protocol.ApprovalRecord, error) {
	if filter.Status == "" {
		return f.records, nil
	}
	var out []protocol.ApprovalRecord
	for _, r := range f.records {
		if string(r.Status) == filter.Status {
			out = append(out, r)
		}
	}
	return out, nil
}

func TestApprovalsHandlerIncludesResolveHintOnlyForPending(t *testing.T) {
	reader := filteringApprovalReader{records: []protocol.ApprovalRecord{
		{Id: "appr-pending", RunId: "run-1", Operation: protocol.ApprovalRecordOperationGitPush, RequestedBy: protocol.ApprovalRecordRequestedByPetruk, Status: protocol.ApprovalRecordStatusPending, CreatedAt: time.Now().UTC()},
		{Id: "appr-approved", RunId: "run-1", Operation: protocol.ApprovalRecordOperationGitPush, RequestedBy: protocol.ApprovalRecordRequestedByPetruk, Status: protocol.ApprovalRecordStatusApproved, CreatedAt: time.Now().UTC()},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/approvals", nil)
	rec := httptest.NewRecorder()
	ApprovalsHandler(reader)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("items = %+v, want 2", body.Items)
	}

	byID := map[string]map[string]any{}
	for _, item := range body.Items {
		byID[item["id"].(string)] = item
	}
	if hint, ok := byID["appr-pending"]["approve_command"]; !ok || hint == "" {
		t.Fatalf("appr-pending: approve_command = %v, want a non-empty CLI hint", hint)
	}
	if _, ok := byID["appr-approved"]["approve_command"]; ok {
		t.Fatalf("appr-approved: approve_command present, want omitted for a resolved approval")
	}
}

func TestApprovalsHandlerFiltersByStatus(t *testing.T) {
	reader := filteringApprovalReader{records: []protocol.ApprovalRecord{
		{Id: "appr-pending", RunId: "run-1", Operation: protocol.ApprovalRecordOperationGitPush, RequestedBy: protocol.ApprovalRecordRequestedByPetruk, Status: protocol.ApprovalRecordStatusPending, CreatedAt: time.Now().UTC()},
		{Id: "appr-approved", RunId: "run-1", Operation: protocol.ApprovalRecordOperationGitPush, RequestedBy: protocol.ApprovalRecordRequestedByPetruk, Status: protocol.ApprovalRecordStatusApproved, CreatedAt: time.Now().UTC()},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/approvals?status=pending", nil)
	rec := httptest.NewRecorder()
	ApprovalsHandler(reader)(rec, req)

	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0]["id"] != "appr-pending" {
		t.Fatalf("items = %+v, want only appr-pending", body.Items)
	}
}
