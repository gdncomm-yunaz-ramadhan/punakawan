package mcpserver

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func newIngestRequirementTestStore(t *testing.T) *knowledge.Store {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}
	dir := t.TempDir()
	sup := tools.New(dir)
	store, err := knowledge.Open(sup, filepath.Join(dir, "knowledge"))
	if err != nil {
		t.Fatalf("knowledge.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	})
	return store
}

const getJiraIssueResponseJSON = `{
	"normalized": {
		"source": {
			"provider": "jira",
			"external_id": "PAY-1842",
			"uri": "jira://cloud-1/PAY-1842",
			"retrieved_at": "2026-07-01T00:00:00Z"
		},
		"key": "PAY-1842",
		"summary": "Refund an approved order",
		"status": "Open"
	},
	"raw": {"status": 200, "data": {"key": "PAY-1842", "fields": {"summary": "Refund an approved order"}}}
}`

func newIngestRequirementTestGate(t *testing.T, responses map[string]string) *adapters.Gate {
	t.Helper()
	store, err := approvals.Open(t.TempDir())
	if err != nil {
		t.Fatalf("approvals.Open: %v", err)
	}
	manifest := protocol.AdapterManifest{
		Id:       "atlassian",
		Name:     "atlassian",
		Version:  "0.1.0",
		Protocol: "punakawan.adapter/v1",
		Runtime:  protocol.AdapterManifestRuntimeNode,
		Provides: []string{"jira"},
		Permissions: protocol.AdapterManifestPermissions{
			Network:    protocol.AdapterManifestPermissionsNetwork{Hosts: []string{"api.atlassian.com"}},
			Filesystem: protocol.AdapterManifestPermissionsFilesystem{Read: []string{}, Write: []string{}},
			Secrets:    []string{},
		},
		Operations: protocol.AdapterManifestOperations{
			"atlassian.getJiraIssue": {SideEffect: false},
		},
	}
	fc := &fakeAtlassianCaller{responses: responses}
	return adapters.NewGate("atlassian", manifest, fc, store)
}

func TestIngestJiraRequirementCreatesRecord(t *testing.T) {
	store := newIngestRequirementTestStore(t)
	gate := newIngestRequirementTestGate(t, map[string]string{"atlassian.getJiraIssue": getJiraIssueResponseJSON})
	in := IngestJiraRequirementInput{RunId: "run-1", IssueIdOrKey: "PAY-1842", RequestedBy: "semar"}

	out, err := ingestJiraRequirement(context.Background(), nil, gate, store, "smoke", in)
	if err != nil {
		t.Fatalf("ingestJiraRequirement: %v", err)
	}
	if !out.Created {
		t.Fatal("Created = false, want true for a first ingestion")
	}
	if out.RequirementId != "pkw:req/smoke/PAY-1842" {
		t.Fatalf("RequirementId = %q, want pkw:req/smoke/PAY-1842", out.RequirementId)
	}
	if out.Title != "Refund an approved order" {
		t.Fatalf("Title = %q, want %q", out.Title, "Refund an approved order")
	}
	if out.Status != "Open" {
		t.Fatalf("Status = %q, want Open", out.Status)
	}

	rec, err := store.Get(out.RequirementId)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.Type != protocol.KnowledgeRecordTypeRequirement {
		t.Fatalf("Type = %q, want requirement", rec.Type)
	}
	if rec.Source.ContentHash == nil || *rec.Source.ContentHash == "" {
		t.Fatal("Source.ContentHash is empty, want a baseline hash for later reconcile.CheckSourceStale calls")
	}
	if rec.Extraction.Method != protocol.KnowledgeRecordExtractionMethodImported {
		t.Fatalf("Extraction.Method = %q, want imported", rec.Extraction.Method)
	}
	if rec.Validity.State != protocol.KnowledgeRecordValidityStateObserved {
		t.Fatalf("Validity.State = %q, want observed", rec.Validity.State)
	}

	// This is the bug this tool fixes: the requirement_id it returns must
	// be immediately usable by build_task_context/submit_task_graph's
	// store.Get, not just by this test's own store handle.
	if _, err := store.Get("pkw:req/smoke/PAY-1842"); err != nil {
		t.Fatalf("requirement record not retrievable by its own id: %v", err)
	}
}

func TestIngestJiraRequirementRefreshesExistingRecord(t *testing.T) {
	store := newIngestRequirementTestStore(t)
	gate := newIngestRequirementTestGate(t, map[string]string{"atlassian.getJiraIssue": getJiraIssueResponseJSON})
	in := IngestJiraRequirementInput{RunId: "run-1", IssueIdOrKey: "PAY-1842", RequestedBy: "semar"}

	if _, err := ingestJiraRequirement(context.Background(), nil, gate, store, "smoke", in); err != nil {
		t.Fatalf("first ingest: %v", err)
	}

	out, err := ingestJiraRequirement(context.Background(), nil, gate, store, "smoke", in)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if out.Created {
		t.Fatal("Created = true on second ingest of the same issue, want false")
	}
}

func TestIngestJiraRequirementRejectsMissingSummary(t *testing.T) {
	store := newIngestRequirementTestStore(t)
	gate := newIngestRequirementTestGate(t, map[string]string{
		"atlassian.getJiraIssue": `{"normalized":{"source":{"provider":"jira"},"key":"PAY-2","summary":"","status":"Open"}}`,
	})
	in := IngestJiraRequirementInput{RunId: "run-1", IssueIdOrKey: "PAY-2", RequestedBy: "semar"}

	if _, err := ingestJiraRequirement(context.Background(), nil, gate, store, "smoke", in); err == nil {
		t.Fatal("expected an error for a Jira issue with no summary")
	}
}
