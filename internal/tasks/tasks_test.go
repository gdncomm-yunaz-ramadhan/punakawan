package tasks

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// requireBinary skips the test if name is not on PATH, mirroring
// internal/knowledge/service_test.go's newTestStore skip pattern for dolt.
func requireBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skip(name + " not installed")
	}
}

// newTestStore opens a Dolt-backed knowledge Store rooted at t.TempDir(),
// never touching this repository's own .beads/ or knowledge data.
func newTestStore(t *testing.T, sup *tools.Supervisor, root string) *knowledge.Store {
	t.Helper()
	store, err := knowledge.Open(sup, filepath.Join(root, "knowledge"))
	if err != nil {
		t.Fatalf("knowledge.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Logf("Store.Close: %v", err)
		}
	})
	return store
}

// newTestBeadsProject initializes a throwaway bd project rooted at root.
func newTestBeadsProject(t *testing.T, sup *tools.Supervisor, root string) {
	t.Helper()
	res, err := sup.Run(context.Background(), tools.Spec{
		Name: "bd",
		Args: []string{"init", "--non-interactive", "--prefix", "test", "--skip-agents", "--skip-hooks", "-q"},
		Dir:  root,
	})
	if err != nil {
		t.Fatalf("bd init: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("bd init failed: %s", res.Stderr)
	}
}

func requirementRecord() protocol.KnowledgeRecord {
	return protocol.KnowledgeRecord{
		Id:     "pkw:req/checkout-platform/REQ-2026-0182",
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: "active",
		Title:  "Refund approved order",
		Source: protocol.KnowledgeRecordSource{
			Provider:    "jira",
			ExternalId:  strPtr("PAY-1842"),
			RetrievedAt: time.Now().UTC(),
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodModelAssisted,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State: protocol.KnowledgeRecordValidityStateInferred,
		},
	}
}

func strPtr(s string) *string { return &s }

// TestBeadsDependencyTypeMapping does not require dolt or bd; it exercises
// the pure translation function directly.
func TestBeadsDependencyTypeMapping(t *testing.T) {
	cases := []struct {
		in   protocol.TaskContractDependenciesElemType
		want string
	}{
		{protocol.TaskContractDependenciesElemTypeBlocks, "blocks"},
		{protocol.TaskContractDependenciesElemTypeDiscoveredFrom, "discovered-from"},
		{protocol.TaskContractDependenciesElemTypeRequires, "blocks"},
	}
	for _, c := range cases {
		got, err := beadsDependencyType(c.in)
		if err != nil {
			t.Fatalf("beadsDependencyType(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("beadsDependencyType(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	if _, err := beadsDependencyType("bogus"); err == nil {
		t.Fatal("expected error for unknown dependency type")
	}
}

// TestCreateTaskForRequirementRejectsNonRequirement does not require dolt or
// bd: it exercises the type guard before any external process would be
// invoked.
func TestCreateTaskForRequirementRejectsNonRequirement(t *testing.T) {
	rec := requirementRecord()
	rec.Type = protocol.KnowledgeRecordTypeDecision

	_, err := CreateTaskForRequirement(context.Background(), nil, "", nil, rec, NewTaskContractInput{
		TaskID:             "task-1",
		AcceptanceCriteria: []string{"criterion"},
	})
	if err == nil {
		t.Fatal("expected error for non-requirement record")
	}
}

func TestCreateTaskForRequirementRequiresAcceptanceCriteria(t *testing.T) {
	rec := requirementRecord()
	_, err := CreateTaskForRequirement(context.Background(), nil, "", nil, rec, NewTaskContractInput{
		TaskID: "task-1",
	})
	if err == nil {
		t.Fatal("expected error for missing acceptance criteria")
	}
}

func TestCreateTaskForRequirementRequiresTaskID(t *testing.T) {
	rec := requirementRecord()
	_, err := CreateTaskForRequirement(context.Background(), nil, "", nil, rec, NewTaskContractInput{
		AcceptanceCriteria: []string{"criterion"},
	})
	if err == nil {
		t.Fatal("expected error for missing task id")
	}
}

// TestCreateTaskForRequirementEndToEnd exercises the full path: bd issue
// creation plus persisting the tracked-by relation into a real Dolt-backed
// knowledge Store. It requires both dolt and bd on PATH.
func TestCreateTaskForRequirementEndToEnd(t *testing.T) {
	requireBinary(t, "dolt")
	requireBinary(t, "bd")

	root := t.TempDir()
	sup := tools.New(root)

	store := newTestStore(t, sup, root)
	newTestBeadsProject(t, sup, root)

	req := requirementRecord()
	if err := store.Put(req); err != nil {
		t.Fatalf("seed requirement Put: %v", err)
	}

	approvalRequired := true
	contract, err := CreateTaskForRequirement(context.Background(), sup, root, store, req, NewTaskContractInput{
		TaskID:                    "task-refund-api",
		Repository:                "checkout-platform",
		Scope:                     "Implement refund API behavior",
		ExpectedFilesOrComponents: []string{"internal/refund/api.go"},
		AcceptanceCriteria:        []string{"Refund endpoint returns 200 on success", "Refund is idempotent"},
		TestRequirements:          []string{"unit tests for refund handler"},
		RequiredEvidence:          []string{"test output", "coverage report"},
		RiskClassification:        protocol.TaskContractRiskClassificationMedium,
		ApprovalRequired:          &approvalRequired,
		DefinitionOfDone:          "Refund API implemented, tested, and reviewed",
		BeadsType:                 "task",
		BeadsLabels:               []string{"requirement"},
	})
	if err != nil {
		t.Fatalf("CreateTaskForRequirement: %v", err)
	}

	if contract.RequirementId != req.Id {
		t.Fatalf("RequirementId = %q, want %q", contract.RequirementId, req.Id)
	}
	if contract.JiraKey == nil || *contract.JiraKey != "PAY-1842" {
		t.Fatalf("JiraKey = %v, want PAY-1842", contract.JiraKey)
	}
	if contract.BeadsEpic == nil || *contract.BeadsEpic == "" {
		t.Fatal("expected BeadsEpic to be set to the created bd issue id")
	}

	// The requirement record's own relations must now include a tracked-by
	// edge to the new Beads issue id, persisted via store.Put.
	got, err := store.Get(req.Id)
	if err != nil {
		t.Fatalf("Get requirement: %v", err)
	}
	found := false
	for _, rel := range got.Relations {
		if rel.Type == protocol.KnowledgeRecordRelationsElemTypeTrackedBy && rel.Target == *contract.BeadsEpic {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a tracked-by relation to %s, got %+v", *contract.BeadsEpic, got.Relations)
	}

	// Create a second task for the same requirement and wire a "blocks"
	// dependency onto the first, exercising WireDependency end-to-end
	// (§10.2's task graph example: one task blocking another).
	req2 := req
	req2.Id = "pkw:req/checkout-platform/REQ-2026-0184"
	if err := store.Put(req2); err != nil {
		t.Fatalf("seed second requirement Put: %v", err)
	}
	contract2, err := CreateTaskForRequirement(context.Background(), sup, root, store, req2, NewTaskContractInput{
		TaskID:             "task-refund-migration",
		Repository:         "checkout-platform",
		Scope:              "Create database migration",
		AcceptanceCriteria: []string{"Migration applies cleanly"},
		DefinitionOfDone:   "Migration merged",
		BeadsType:          "task",
	})
	if err != nil {
		t.Fatalf("CreateTaskForRequirement (second): %v", err)
	}

	if err := WireDependency(context.Background(), sup, root, *contract.BeadsEpic, *contract2.BeadsEpic, protocol.TaskContractDependenciesElemTypeBlocks); err != nil {
		t.Fatalf("WireDependency: %v", err)
	}
}

// TestCreateTaskForRequirementNonJiraSourceLeavesJiraKeyEmpty confirms
// §10.1's jira_key is only populated for a Jira-sourced requirement.
func TestCreateTaskForRequirementNonJiraSourceLeavesJiraKeyEmpty(t *testing.T) {
	requireBinary(t, "dolt")
	requireBinary(t, "bd")

	root := t.TempDir()
	sup := tools.New(root)

	store := newTestStore(t, sup, root)
	newTestBeadsProject(t, sup, root)

	req := requirementRecord()
	req.Id = "pkw:req/checkout-platform/REQ-2026-0183"
	req.Source.Provider = "confluence"
	req.Source.ExternalId = strPtr("CONF-999")
	if err := store.Put(req); err != nil {
		t.Fatalf("seed requirement Put: %v", err)
	}

	contract, err := CreateTaskForRequirement(context.Background(), sup, root, store, req, NewTaskContractInput{
		TaskID:             "task-non-jira",
		Repository:         "checkout-platform",
		Scope:              "scope",
		AcceptanceCriteria: []string{"criterion"},
		DefinitionOfDone:   "done",
	})
	if err != nil {
		t.Fatalf("CreateTaskForRequirement: %v", err)
	}
	if contract.JiraKey != nil {
		t.Fatalf("expected JiraKey to be nil for non-jira source, got %v", *contract.JiraKey)
	}
}
