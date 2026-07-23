package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func strPtr(s string) *string { return &s }

type fakeKnowledgeReader struct {
	records []protocol.KnowledgeRecord
	results []search.Result
	detail  protocol.KnowledgeRecord
	related []protocol.KnowledgeRecord
	history []knowledge.Event
}

func (f fakeKnowledgeReader) List(ctx context.Context, workspaceID string, filter contract.KnowledgeFilter) ([]protocol.KnowledgeRecord, error) {
	var out []protocol.KnowledgeRecord
	for _, r := range f.records {
		if filter.Type != "" && string(r.Type) != filter.Type {
			continue
		}
		if filter.Repository != "" && (r.Scope == nil || r.Scope.Repository == nil || *r.Scope.Repository != filter.Repository) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (f fakeKnowledgeReader) Search(ctx context.Context, workspaceID string, req search.Request) ([]search.Result, error) {
	return f.results, nil
}

func (f fakeKnowledgeReader) Get(ctx context.Context, workspaceID, knowledgeID string) (protocol.KnowledgeRecord, error) {
	if knowledgeID != f.detail.Id {
		return protocol.KnowledgeRecord{}, errors.New("not found")
	}
	return f.detail, nil
}

func (f fakeKnowledgeReader) Relations(ctx context.Context, workspaceID, knowledgeID string) ([]protocol.KnowledgeRecord, error) {
	return f.related, nil
}

func (f fakeKnowledgeReader) History(ctx context.Context, workspaceID, knowledgeID string) ([]knowledge.Event, error) {
	return f.history, nil
}

type fakeGlobalSearchReader struct {
	results []contract.GlobalSearchResult
}

func (f fakeGlobalSearchReader) Search(ctx context.Context, req search.Request) ([]contract.GlobalSearchResult, error) {
	return f.results, nil
}

func testKnowledgeRecord(id, repository string) protocol.KnowledgeRecord {
	return protocol.KnowledgeRecord{
		Id:         id,
		Type:       protocol.KnowledgeRecordTypeRequirement,
		Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodManual},
		Validity:   protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateVerified},
		Scope:      &protocol.KnowledgeRecordScope{Repository: strPtr(repository)},
	}
}

func TestKnowledgeListHandlerFiltersByRepository(t *testing.T) {
	reader := fakeKnowledgeReader{records: []protocol.KnowledgeRecord{
		testKnowledgeRecord("pkw:requirement/repo-a/x", "repo-a"),
		testKnowledgeRecord("pkw:requirement/repo-b/y", "repo-b"),
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/knowledge?repository=repo-a", nil)
	rec := httptest.NewRecorder()
	KnowledgeListHandler(reader)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items []protocol.KnowledgeRecord `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Id != "pkw:requirement/repo-a/x" {
		t.Fatalf("items = %+v, want only repo-a's record", body.Items)
	}
}

func TestKnowledgeListHandlerWithQueryDelegatesToSearch(t *testing.T) {
	reader := fakeKnowledgeReader{
		records: []protocol.KnowledgeRecord{testKnowledgeRecord("pkw:requirement/repo-a/x", "repo-a")},
		results: []search.Result{{Id: "pkw:requirement/repo-a/x", Title: "match", Record: testKnowledgeRecord("pkw:requirement/repo-a/x", "repo-a")}},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/knowledge?q=refund", nil)
	rec := httptest.NewRecorder()
	KnowledgeListHandler(reader)(rec, req)

	var body struct {
		Items []search.Result `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Title != "match" {
		t.Fatalf("items = %+v, want the search result, not the raw record list", body.Items)
	}
}

func TestKnowledgeHandlerUnknownIDReturns404(t *testing.T) {
	reader := fakeKnowledgeReader{detail: protocol.KnowledgeRecord{Id: "pkw:requirement/repo-a/x"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/knowledge/no-such-id", nil)
	req.SetPathValue("knowledgeId", "no-such-id")
	rec := httptest.NewRecorder()
	KnowledgeHandler(reader)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestKnowledgeHistoryHandlerReturnsEvents(t *testing.T) {
	now := time.Now().UTC()
	reader := fakeKnowledgeReader{history: []knowledge.Event{
		{Type: knowledge.EventTypePut, RecordId: "pkw:requirement/repo-a/x", RecordType: protocol.KnowledgeRecordTypeRequirement, Timestamp: now},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/knowledge/x/history", nil)
	rec := httptest.NewRecorder()
	KnowledgeHistoryHandler(reader)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items []knowledge.Event `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Type != knowledge.EventTypePut {
		t.Fatalf("items = %+v, want the put event", body.Items)
	}
}

func TestGlobalSearchHandlerReturnsFusedResults(t *testing.T) {
	reader := fakeGlobalSearchReader{results: []contract.GlobalSearchResult{
		{WorkspaceID: "ws-a", Result: search.Result{Id: "pkw:requirement/repo-a/x", Record: testKnowledgeRecord("pkw:requirement/repo-a/x", "repo-a")}, RRFScore: 0.016},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=refund", nil)
	rec := httptest.NewRecorder()
	GlobalSearchHandler(reader)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items []contract.GlobalSearchResult `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].WorkspaceID != "ws-a" {
		t.Fatalf("items = %+v, want the ws-a result", body.Items)
	}
}

func TestParseSearchRequestParsesTypeAndRepo(t *testing.T) {
	req := parseSearchRequest(map[string][]string{
		"q":    {"refund policy"},
		"type": {"requirement,claim"},
		"repo": {"checkout-api"},
	})
	if req.Query != "refund policy" {
		t.Fatalf("Query = %q, want %q", req.Query, "refund policy")
	}
	if len(req.Types) != 2 || req.Types[0] != "requirement" || req.Types[1] != "claim" {
		t.Fatalf("Types = %+v, want [requirement claim]", req.Types)
	}
	if req.Scope.Repository != "checkout-api" {
		t.Fatalf("Scope.Repository = %q, want checkout-api", req.Scope.Repository)
	}
}

func TestKnowledgeDetailHandlerRoutesBySuffixDespiteSlashesInID(t *testing.T) {
	// Knowledge IDs contain literal slashes (pkw:<type>/<repo>/<name>), so
	// the whole remainder after "/knowledge/" is captured as one
	// wildcard path value; this handler must still tell detail, relations,
	// and history apart by peeling a known suffix off that value.
	id := "pkw:requirement/repo-a/refund-sla"
	reader := fakeKnowledgeReader{
		detail:  testKnowledgeRecord(id, "repo-a"),
		related: []protocol.KnowledgeRecord{testKnowledgeRecord("pkw:requirement/repo-a/other", "repo-a")},
		history: []knowledge.Event{{Type: knowledge.EventTypePut, RecordId: id, RecordType: protocol.KnowledgeRecordTypeRequirement, Timestamp: time.Now().UTC()}},
	}
	handler := KnowledgeDetailHandler(reader)

	call := func(rest string) map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-a/knowledge/"+rest, nil)
		req.SetPathValue("knowledgeRest", rest)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("rest=%q: status = %d, want 200", rest, rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("rest=%q: decode: %v", rest, err)
		}
		return body
	}

	if got := call(id); got["id"] != id {
		t.Fatalf("detail: id = %v, want %v", got["id"], id)
	}
	if got := call(id + "/relations"); got["items"] == nil {
		t.Fatalf("relations: got %+v, want an items field", got)
	}
	if got := call(id + "/history"); got["items"] == nil {
		t.Fatalf("history: got %+v, want an items field", got)
	}
}
