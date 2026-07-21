package reconcile

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

type fakeGate struct {
	responses map[string]json.RawMessage
	calls     []string
}

func (f *fakeGate) Call(ctx context.Context, runID, op string, params map[string]any) (json.RawMessage, error) {
	f.calls = append(f.calls, op)
	return f.responses[op], nil
}

func newTestStore(t *testing.T) *knowledge.Store {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}
	dir := t.TempDir()
	sup := tools.New(dir)
	store, err := knowledge.Open(sup, filepath.Join(dir, "knowledge"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	})
	return store
}

func jiraRecord(t *testing.T, store *knowledge.Store, hash string) protocol.KnowledgeRecord {
	t.Helper()
	externalID := "PAY-1842"
	contentHash := hash
	rec := protocol.KnowledgeRecord{
		Id:     "pkw:req/fixture/REQ-1",
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: "active",
		Title:  "Refund approved order",
		Source: protocol.KnowledgeRecordSource{
			Provider:    "jira",
			ExternalId:  &externalID,
			ContentHash: &contentHash,
			RetrievedAt: time.Now().UTC(),
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodImported,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State: protocol.KnowledgeRecordValidityStateObserved,
		},
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	return rec
}

func TestCheckSourceStaleUnchangedContent(t *testing.T) {
	store := newTestStore(t)
	body := json.RawMessage(`{"summary":"Refund approved order","status":"Open"}`)
	rec := jiraRecord(t, store, knowledge.ContentHash(body))

	gate := &fakeGate{responses: map[string]json.RawMessage{"atlassian.getJiraIssue": body}}
	stale, err := CheckSourceStale(context.Background(), store, gate, "run-1", rec)
	if err != nil {
		t.Fatalf("CheckSourceStale: %v", err)
	}
	if stale {
		t.Fatal("expected stale=false when content is unchanged")
	}
	if len(gate.calls) != 1 || gate.calls[0] != "atlassian.getJiraIssue" {
		t.Fatalf("calls = %+v, want one atlassian.getJiraIssue call", gate.calls)
	}
}

func TestCheckSourceStaleChangedContent(t *testing.T) {
	store := newTestStore(t)
	original := json.RawMessage(`{"summary":"Refund approved order","status":"Open"}`)
	rec := jiraRecord(t, store, knowledge.ContentHash(original))

	changed := json.RawMessage(`{"summary":"Refund approved order","status":"Resolved"}`)
	gate := &fakeGate{responses: map[string]json.RawMessage{"atlassian.getJiraIssue": changed}}

	stale, err := CheckSourceStale(context.Background(), store, gate, "run-1", rec)
	if err != nil {
		t.Fatalf("CheckSourceStale: %v", err)
	}
	if !stale {
		t.Fatal("expected stale=true when content changed")
	}

	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Validity.State != protocol.KnowledgeRecordValidityStateStale {
		t.Fatalf("Validity.State = %q, want stale", got.Validity.State)
	}
}

func TestFetchOpForSourceRequiresExternalID(t *testing.T) {
	_, _, err := fetchOpForSource(protocol.KnowledgeRecordSource{Provider: "jira"})
	if err == nil {
		t.Fatal("expected error when source.external_id is missing")
	}
}

func TestFetchOpForJiraSourceRequestsRawAllFieldsForReconciliation(t *testing.T) {
	externalID := "PAY-1"
	op, params, err := fetchOpForSource(protocol.KnowledgeRecordSource{Provider: "jira", ExternalId: &externalID})
	if err != nil {
		t.Fatalf("fetchOpForSource: %v", err)
	}
	fields, ok := params["fields"].([]string)
	if op != "atlassian.getJiraIssue" || params["includeRaw"] != true || !ok || len(fields) != 1 || fields[0] != "*all" {
		t.Fatalf("op=%q params=%+v", op, params)
	}
}

func TestFetchOpForSourceUnsupportedProvider(t *testing.T) {
	externalID := "x"
	_, _, err := fetchOpForSource(protocol.KnowledgeRecordSource{Provider: "docling", ExternalId: &externalID})
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestFetchOpForSourceConfluence(t *testing.T) {
	externalID := "123456"
	op, params, err := fetchOpForSource(protocol.KnowledgeRecordSource{Provider: "confluence", ExternalId: &externalID})
	if err != nil {
		t.Fatalf("fetchOpForSource: %v", err)
	}
	if op != "atlassian.getConfluencePage" || params["pageId"] != externalID || params["includeRaw"] != true {
		t.Fatalf("op=%q params=%+v", op, params)
	}
}

func TestStableSourcePayloadIgnoresChangingRetrievalMetadata(t *testing.T) {
	first := json.RawMessage(`{"normalized":{"source":{"retrieved_at":"2026-07-21T01:00:00Z"}},"raw":{"status":200,"data":{"key":"PAY-1","fields":{"status":{"name":"Open"}}}}}`)
	second := json.RawMessage(`{"normalized":{"source":{"retrieved_at":"2026-07-21T02:00:00Z"}},"raw":{"status":200,"data":{"key":"PAY-1","fields":{"status":{"name":"Open"}}}}}`)
	if knowledge.ContentHash(stableSourcePayload(first)) != knowledge.ContentHash(stableSourcePayload(second)) {
		t.Fatal("retrieval metadata must not change the source content hash")
	}
}
