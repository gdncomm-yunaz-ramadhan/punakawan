package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

type fakeEvidenceReader struct {
	list    []protocol.EvidenceRecord
	detail  protocol.EvidenceRecord
	preview contract.EvidencePreview
}

func (f fakeEvidenceReader) List(ctx context.Context, workspaceID, sessionID string) ([]protocol.EvidenceRecord, error) {
	return f.list, nil
}

func (f fakeEvidenceReader) Get(ctx context.Context, workspaceID, evidenceID string) (protocol.EvidenceRecord, error) {
	if evidenceID != f.detail.Id {
		return protocol.EvidenceRecord{}, errors.New("not found")
	}
	return f.detail, nil
}

func (f fakeEvidenceReader) Preview(ctx context.Context, workspaceID, evidenceID string, offset, limit int64) (contract.EvidencePreview, error) {
	if evidenceID != f.detail.Id {
		return contract.EvidencePreview{}, errors.New("not found")
	}
	return f.preview, nil
}

func TestEvidenceListHandlerReturnsItems(t *testing.T) {
	reader := fakeEvidenceReader{list: []protocol.EvidenceRecord{
		{Id: "ev-1", RunId: "run-1", Type: protocol.EvidenceRecordTypeCommandOutput, CreatedAt: time.Now().UTC()},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/sessions/run-1/evidence", nil)
	rec := httptest.NewRecorder()
	EvidenceListHandler(reader)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items []protocol.EvidenceRecord `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Id != "ev-1" {
		t.Fatalf("items = %+v, want ev-1", body.Items)
	}
}

func TestEvidenceHandlerUnknownIDReturns404(t *testing.T) {
	reader := fakeEvidenceReader{detail: protocol.EvidenceRecord{Id: "ev-1"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/evidence/no-such-id", nil)
	req.SetPathValue("evidenceId", "no-such-id")
	rec := httptest.NewRecorder()
	EvidenceHandler(reader)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestEvidencePreviewHandlerReturnsJSONForText(t *testing.T) {
	reader := fakeEvidenceReader{
		detail:  protocol.EvidenceRecord{Id: "ev-1"},
		preview: contract.EvidencePreview{Kind: "text", MimeType: "text/plain", Data: []byte("hello"), TotalSize: 5},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/evidence/ev-1/preview", nil)
	req.SetPathValue("evidenceId", "ev-1")
	rec := httptest.NewRecorder()
	EvidencePreviewHandler(reader)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want JSON", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["text"] != "hello" {
		t.Fatalf("text = %v, want hello", body["text"])
	}
}

func TestEvidencePreviewHandlerServesRawBytesForBinary(t *testing.T) {
	reader := fakeEvidenceReader{
		detail:  protocol.EvidenceRecord{Id: "ev-shot"},
		preview: contract.EvidencePreview{Kind: "binary", MimeType: "image/png", Data: []byte("pngbytes"), TotalSize: 8},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/evidence/ev-shot/preview", nil)
	req.SetPathValue("evidenceId", "ev-shot")
	rec := httptest.NewRecorder()
	EvidencePreviewHandler(reader)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	if rec.Body.String() != "pngbytes" {
		t.Fatalf("body = %q, want raw bytes", rec.Body.String())
	}
}

func TestEvidencePreviewHandlerParsesOffsetAndLimit(t *testing.T) {
	var gotOffset, gotLimit int64
	reader := fakeEvidenceReaderFunc(func(ctx context.Context, workspaceID, evidenceID string, offset, limit int64) (contract.EvidencePreview, error) {
		gotOffset, gotLimit = offset, limit
		return contract.EvidencePreview{Kind: "text"}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/evidence/ev-1/preview?offset=128&limit=64", nil)
	req.SetPathValue("evidenceId", "ev-1")
	rec := httptest.NewRecorder()
	EvidencePreviewHandler(reader)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotOffset != 128 || gotLimit != 64 {
		t.Fatalf("offset,limit = %d,%d, want 128,64", gotOffset, gotLimit)
	}
}

// fakeEvidenceReaderFunc lets TestEvidencePreviewHandlerParsesOffsetAndLimit
// assert on the offset/limit EvidencePreviewHandler actually parsed and
// forwarded, without needing a stateful fake.
type fakeEvidenceReaderFunc func(ctx context.Context, workspaceID, evidenceID string, offset, limit int64) (contract.EvidencePreview, error)

func (f fakeEvidenceReaderFunc) List(ctx context.Context, workspaceID, sessionID string) ([]protocol.EvidenceRecord, error) {
	return nil, nil
}

func (f fakeEvidenceReaderFunc) Get(ctx context.Context, workspaceID, evidenceID string) (protocol.EvidenceRecord, error) {
	return protocol.EvidenceRecord{}, nil
}

func (f fakeEvidenceReaderFunc) Preview(ctx context.Context, workspaceID, evidenceID string, offset, limit int64) (contract.EvidencePreview, error) {
	return f(ctx, workspaceID, evidenceID, offset, limit)
}
