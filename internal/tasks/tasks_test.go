package tasks

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

// TestReportDiscoveredWorkEndToEnd exercises §10.4's discovery rule: the
// resulting TaskContract.DiscoveredFrom must point at the discovering task,
// and the created Beads issue must carry both "discovered" and
// "needs-semar-review" labels (in addition to any caller-supplied labels)
// so Semar can find it via `bd list --label needs-semar-review` without a
// dedicated review-queue subsystem.
func TestReportDiscoveredWorkEndToEnd(t *testing.T) {
	requireBinary(t, "dolt")
	requireBinary(t, "bd")

	root := t.TempDir()
	sup := tools.New(root)

	store := newTestStore(t, sup, root)
	newTestBeadsProject(t, sup, root)

	req := requirementRecord()
	req.Id = "pkw:req/checkout-platform/REQ-2026-0185"
	if err := store.Put(req); err != nil {
		t.Fatalf("seed requirement Put: %v", err)
	}

	const discoveringTaskID = "task-refund-api"
	contract, err := ReportDiscoveredWork(context.Background(), sup, root, store, req, discoveringTaskID, NewTaskContractInput{
		TaskID:             "task-refund-retry-handling",
		Repository:         "checkout-platform",
		Scope:              "Handle refund gateway timeouts discovered while implementing the refund API",
		AcceptanceCriteria: []string{"Refund retries on gateway timeout"},
		DefinitionOfDone:   "Retry handling implemented and tested",
		BeadsType:          "task",
		BeadsLabels:        []string{"requirement"},
	})
	if err != nil {
		t.Fatalf("ReportDiscoveredWork: %v", err)
	}

	if contract.DiscoveredFrom == nil || *contract.DiscoveredFrom != discoveringTaskID {
		t.Fatalf("DiscoveredFrom = %v, want %q", contract.DiscoveredFrom, discoveringTaskID)
	}
	if contract.BeadsEpic == nil || *contract.BeadsEpic == "" {
		t.Fatal("expected BeadsEpic to be set to the created bd issue id")
	}

	res, err := sup.Run(context.Background(), tools.Spec{
		Name: "bd",
		Args: []string{"show", *contract.BeadsEpic, "--json"},
		Dir:  root,
	})
	if err != nil {
		t.Fatalf("bd show: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("bd show failed: %s", res.Stderr)
	}

	// bd show --json emits an array of issues (one per requested id), unlike
	// bd create --json's single object.
	var shown []struct {
		Labels []string `json:"labels"`
	}
	if err := json.Unmarshal(res.Stdout, &shown); err != nil {
		t.Fatalf("decode bd show output: %v", err)
	}
	if len(shown) != 1 {
		t.Fatalf("expected exactly one issue from bd show, got %d", len(shown))
	}
	for _, want := range []string{"requirement", "discovered", "needs-semar-review"} {
		found := false
		for _, l := range shown[0].Labels {
			if l == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected label %q on created issue, got %v", want, shown[0].Labels)
		}
	}
}

// TestGenerateGraphRejectsUnknownLocalKey confirms a DependsOn edge onto a
// nonexistent LocalKey is rejected before anything is created (no dolt/bd
// required, since the failure happens in the up-front validation pass).
func TestGenerateGraphRejectsUnknownLocalKey(t *testing.T) {
	items := []GraphItem{
		{
			LocalKey:      "api",
			RequirementID: "pkw:req/checkout-platform/REQ-2026-0182",
			Input:         NewTaskContractInput{TaskID: "task-api", AcceptanceCriteria: []string{"c"}},
			DependsOn:     []GraphDependency{{LocalKey: "does-not-exist", Type: protocol.TaskContractDependenciesElemTypeBlocks}},
		},
	}
	if _, err := GenerateGraph(context.Background(), nil, "", nil, items); err == nil {
		t.Fatal("expected error for depends_on referencing an unknown local_key")
	}
}

// TestGenerateGraphRejectsDuplicateLocalKey confirms duplicate LocalKeys are
// rejected up front.
func TestGenerateGraphRejectsDuplicateLocalKey(t *testing.T) {
	items := []GraphItem{
		{LocalKey: "api", RequirementID: "req-1", Input: NewTaskContractInput{TaskID: "task-1", AcceptanceCriteria: []string{"c"}}},
		{LocalKey: "api", RequirementID: "req-1", Input: NewTaskContractInput{TaskID: "task-2", AcceptanceCriteria: []string{"c"}}},
	}
	if _, err := GenerateGraph(context.Background(), nil, "", nil, items); err == nil {
		t.Fatal("expected error for duplicate local_key")
	}
}

// TestGenerateGraphEndToEnd exercises §10.2's task graph example: a
// migration task that blocks an API implementation task, generated as a
// single batch with dependencies wired between them.
func TestGenerateGraphEndToEnd(t *testing.T) {
	requireBinary(t, "dolt")
	requireBinary(t, "bd")

	root := t.TempDir()
	sup := tools.New(root)

	store := newTestStore(t, sup, root)
	newTestBeadsProject(t, sup, root)

	req := requirementRecord()
	req.Id = "pkw:req/checkout-platform/REQ-2026-0186"
	if err := store.Put(req); err != nil {
		t.Fatalf("seed requirement Put: %v", err)
	}

	items := []GraphItem{
		{
			LocalKey:      "migration",
			RequirementID: req.Id,
			Input: NewTaskContractInput{
				TaskID:             "task-refund-migration",
				Repository:         "checkout-platform",
				Scope:              "Create database migration",
				AcceptanceCriteria: []string{"Migration applies cleanly"},
				DefinitionOfDone:   "Migration merged",
				BeadsType:          "task",
			},
		},
		{
			LocalKey:      "api",
			RequirementID: req.Id,
			Input: NewTaskContractInput{
				TaskID:             "task-refund-api",
				Repository:         "checkout-platform",
				Scope:              "Implement refund API behavior",
				AcceptanceCriteria: []string{"Refund endpoint returns 200 on success"},
				DefinitionOfDone:   "Refund API implemented and tested",
				BeadsType:          "task",
			},
			DependsOn: []GraphDependency{{LocalKey: "migration", Type: protocol.TaskContractDependenciesElemTypeBlocks}},
		},
	}

	results, err := GenerateGraph(context.Background(), sup, root, store, items)
	if err != nil {
		t.Fatalf("GenerateGraph: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	byKey := make(map[string]GraphResult)
	for _, r := range results {
		byKey[r.LocalKey] = r
	}
	api := byKey["api"]
	migration := byKey["migration"]

	if api.Contract.BeadsEpic == nil || migration.Contract.BeadsEpic == nil {
		t.Fatal("expected both results to have a BeadsEpic set")
	}
	if len(api.Contract.Dependencies) != 1 || api.Contract.Dependencies[0].Id != "task-refund-migration" {
		t.Fatalf("expected api's contract-level dependency to auto-populate with migration's task id, got %+v", api.Contract.Dependencies)
	}

	// Confirm the dependency was actually wired in Beads, not just recorded
	// on the contract: bd show should report the api issue as blocked.
	res, err := sup.Run(context.Background(), tools.Spec{
		Name: "bd",
		Args: []string{"show", *api.Contract.BeadsEpic, "--json"},
		Dir:  root,
	})
	if err != nil {
		t.Fatalf("bd show: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("bd show failed: %s", res.Stderr)
	}
	var shown []struct {
		Dependencies []struct {
			Id             string `json:"id"`
			DependencyType string `json:"dependency_type"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(res.Stdout, &shown); err != nil {
		t.Fatalf("decode bd show output: %v", err)
	}
	if len(shown) != 1 {
		t.Fatalf("expected exactly one issue from bd show, got %d", len(shown))
	}
	found := false
	for _, dep := range shown[0].Dependencies {
		if dep.Id == *migration.Contract.BeadsEpic && dep.DependencyType == "blocks" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a blocks dependency onto %s, got %+v", *migration.Contract.BeadsEpic, shown[0].Dependencies)
	}
}

// TestReportDiscoveredWorkRejectsConflictingDiscoveredFrom confirms that a
// caller-supplied in.DiscoveredFrom which disagrees with the
// discoveredFromTaskID argument is rejected rather than silently
// overwritten. It does not require dolt or bd: the conflict is detected
// before any external process would be invoked.
func TestReportDiscoveredWorkRejectsConflictingDiscoveredFrom(t *testing.T) {
	rec := requirementRecord()
	conflicting := "some-other-task"

	_, err := ReportDiscoveredWork(context.Background(), nil, "", nil, rec, "task-refund-api", NewTaskContractInput{
		TaskID:             "task-1",
		AcceptanceCriteria: []string{"criterion"},
		DiscoveredFrom:     &conflicting,
	})
	if err == nil {
		t.Fatal("expected error for conflicting discovered_from")
	}
}
