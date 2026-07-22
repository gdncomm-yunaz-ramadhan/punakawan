package taskcontext

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
	"gopkg.in/yaml.v3"
)

// newTestStore mirrors internal/dossier's newTestStore helper (itself
// mirroring internal/knowledge's own), skipping if dolt is not installed.
func newTestStore(t *testing.T) *knowledge.Store {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "knowledge")
	sup := tools.New(dir)

	store, err := knowledge.Open(sup, dataDir)
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

func baseRecord(id string, typ protocol.KnowledgeRecordType, title string) protocol.KnowledgeRecord {
	return protocol.KnowledgeRecord{
		Id:     id,
		Type:   typ,
		Status: "active",
		Title:  title,
		Source: protocol.KnowledgeRecordSource{
			Provider:    "punakawan",
			RetrievedAt: time.Now().UTC(),
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodModelAssisted,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State: protocol.KnowledgeRecordValidityStateObserved,
		},
	}
}

func TestBuildAssemblesContext(t *testing.T) {
	store := newTestStore(t)

	req := baseRecord("pkw:requirement/test-ws/REQ-1", protocol.KnowledgeRecordTypeRequirement, "Refunds settle same-day")
	req.Status = "approved"
	if err := store.Put(req); err != nil {
		t.Fatalf("seed requirement Put: %v", err)
	}

	apiContract := baseRecord("pkw:apicontract/test-ws/AC-1", protocol.KnowledgeRecordTypeApiContract, "Refund settlement webhook")
	if err := store.Put(apiContract); err != nil {
		t.Fatalf("seed api-contract Put: %v", err)
	}

	decision := baseRecord("pkw:decision/test-ws/DEC-1", protocol.KnowledgeRecordTypeDecision, "Use gateway webhook for settlement")
	if err := store.Put(decision); err != nil {
		t.Fatalf("seed decision Put: %v", err)
	}

	constraint := baseRecord("pkw:constraint/test-ws/CON-1", protocol.KnowledgeRecordTypeConstraint, "Must not change refund approval flow")
	if err := store.Put(constraint); err != nil {
		t.Fatalf("seed constraint Put: %v", err)
	}

	// A record that relates onto this task's id, standing in for a
	// "previous task output" signal found in the knowledge store (e.g. a
	// discovered-from edge), separate from the caller-supplied
	// PreviousTaskOutputs strings.
	priorOutput := baseRecord("pkw:changeset/test-ws/CS-1", protocol.KnowledgeRecordTypeChangeSet, "Added RefundService.Settle")
	priorOutput.Relations = []protocol.KnowledgeRecordRelationsElem{
		{Type: protocol.KnowledgeRecordRelationsElemTypeDiscoveredFrom, Target: "bd-task-1"},
	}
	if err := store.Put(priorOutput); err != nil {
		t.Fatalf("seed change-set Put: %v", err)
	}

	in := BuildInput{
		TaskID:                        "bd-task-1",
		RequirementID:                 req.Id,
		TaskScope:                     "Implement same-day settlement",
		TaskAcceptanceCriteria:        []string{"Refund settles same day"},
		TaskDefinitionOfDone:          "Merged and deployed",
		TaskExpectedFilesOrComponents: []string{"internal/refund/service.go"},
		AffectedSymbolsAndFiles:       []string{"RefundService.Settle"},
		RequiredTests:                 []string{"TestRefundService_Settle"},
		KnownConstraints:              []string{"Do not change the refund approval flow"},
		PreviousTaskOutputs:           []string{"task bd-task-0: scaffolded RefundService"},
	}

	got, err := Build(context.Background(), store, in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Task definition is copied straight through from BuildInput.
	wantDef := TaskDefinition{
		TaskID:                    "bd-task-1",
		RequirementID:             req.Id,
		Scope:                     "Implement same-day settlement",
		AcceptanceCriteria:        []string{"Refund settles same day"},
		DefinitionOfDone:          "Merged and deployed",
		ExpectedFilesOrComponents: []string{"internal/refund/service.go"},
	}
	if !reflect.DeepEqual(got.TaskDefinition, wantDef) {
		t.Fatalf("TaskDefinition: got %+v, want %+v", got.TaskDefinition, wantDef)
	}

	// Parent requirement is looked up from the store.
	if got.ParentRequirement.ID != req.Id {
		t.Fatalf("ParentRequirement.ID: got %q, want %q", got.ParentRequirement.ID, req.Id)
	}
	if got.ParentRequirement.Title != req.Title {
		t.Fatalf("ParentRequirement.Title: got %q, want %q", got.ParentRequirement.Title, req.Title)
	}
	if got.ParentRequirement.Status != req.Status {
		t.Fatalf("ParentRequirement.Status: got %q, want %q", got.ParentRequirement.Status, req.Status)
	}

	if !containsString(got.RelevantSourceExcerpts, summarize(apiContract)) {
		t.Fatalf("RelevantSourceExcerpts: got %v, want an entry for %q", got.RelevantSourceExcerpts, apiContract.Id)
	}
	if !containsString(got.RelatedDecisions, summarize(decision)) {
		t.Fatalf("RelatedDecisions: got %v, want an entry for %q", got.RelatedDecisions, decision.Id)
	}
	if !equalStrings(got.AffectedSymbolsAndFiles, in.AffectedSymbolsAndFiles) {
		t.Fatalf("AffectedSymbolsAndFiles: got %v, want %v", got.AffectedSymbolsAndFiles, in.AffectedSymbolsAndFiles)
	}
	if !equalStrings(got.RequiredTests, in.RequiredTests) {
		t.Fatalf("RequiredTests: got %v, want %v", got.RequiredTests, in.RequiredTests)
	}

	// KnownConstraints combines the caller-supplied string with the
	// store's "constraint"-typed record.
	if !containsString(got.KnownConstraints, "Do not change the refund approval flow") {
		t.Fatalf("KnownConstraints: got %v, want caller-supplied constraint present", got.KnownConstraints)
	}
	if !containsString(got.KnownConstraints, summarize(constraint)) {
		t.Fatalf("KnownConstraints: got %v, want an entry for %q", got.KnownConstraints, constraint.Id)
	}

	// PreviousTaskOutputs combines the caller-supplied summary with the
	// store's relation-onto-this-task record.
	if !containsString(got.PreviousTaskOutputs, "task bd-task-0: scaffolded RefundService") {
		t.Fatalf("PreviousTaskOutputs: got %v, want caller-supplied summary present", got.PreviousTaskOutputs)
	}
	if !containsString(got.PreviousTaskOutputs, summarize(priorOutput)) {
		t.Fatalf("PreviousTaskOutputs: got %v, want an entry for %q", got.PreviousTaskOutputs, priorOutput.Id)
	}
}

func TestBuildRequiresTaskID(t *testing.T) {
	store := newTestStore(t)
	_, err := Build(context.Background(), store, BuildInput{RequirementID: "pkw:requirement/test-ws/REQ-1"})
	if err == nil {
		t.Fatal("Build: expected error when TaskID is empty")
	}
}

func TestBuildRequiresRequirementID(t *testing.T) {
	store := newTestStore(t)
	_, err := Build(context.Background(), store, BuildInput{TaskID: "bd-task-1"})
	if err == nil {
		t.Fatal("Build: expected error when RequirementID is empty")
	}
}

func TestBuildRejectsUnknownRequirement(t *testing.T) {
	store := newTestStore(t)
	_, err := Build(context.Background(), store, BuildInput{
		TaskID:        "bd-task-1",
		RequirementID: "pkw:requirement/test-ws/does-not-exist",
	})
	if err == nil {
		t.Fatal("Build: expected error for an unknown requirement id")
	}
}

func TestBuildRejectsNonRequirementRecord(t *testing.T) {
	store := newTestStore(t)
	decision := baseRecord("pkw:decision/test-ws/DEC-1", protocol.KnowledgeRecordTypeDecision, "Not a requirement")
	if err := store.Put(decision); err != nil {
		t.Fatalf("seed decision Put: %v", err)
	}

	_, err := Build(context.Background(), store, BuildInput{
		TaskID:        "bd-task-1",
		RequirementID: decision.Id,
	})
	if err == nil {
		t.Fatal("Build: expected error when RequirementID points at a non-requirement record")
	}
}

func TestWriteToBundleWritesYAML(t *testing.T) {
	store := newTestStore(t)

	req := baseRecord("pkw:requirement/test-ws/REQ-2", protocol.KnowledgeRecordTypeRequirement, "Support partial refunds")
	if err := store.Put(req); err != nil {
		t.Fatalf("seed requirement Put: %v", err)
	}

	in := BuildInput{
		TaskID:        "bd-task-2",
		RequirementID: req.Id,
		TaskScope:     "Add partial refund support",
	}
	got, err := Build(context.Background(), store, in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	root := t.TempDir()
	bundle, err := evidence.NewBundle(root, "run-1", "bd-task-2")
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}

	if err := WriteToBundle(got, bundle); err != nil {
		t.Fatalf("WriteToBundle: %v", err)
	}

	path := bundle.Path("task.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}

	var roundTripped Context
	if err := yaml.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(roundTripped.TaskDefinition, got.TaskDefinition) {
		t.Fatalf("round-tripped TaskDefinition: got %+v, want %+v", roundTripped.TaskDefinition, got.TaskDefinition)
	}
	if roundTripped.ParentRequirement != got.ParentRequirement {
		t.Fatalf("round-tripped ParentRequirement: got %+v, want %+v", roundTripped.ParentRequirement, got.ParentRequirement)
	}
}

func TestBuildInheritsOmittedFieldsFromPrevious(t *testing.T) {
	store := newTestStore(t)
	req := baseRecord("pkw:requirement/test-ws/REQ-3", protocol.KnowledgeRecordTypeRequirement, "Resume across rounds")
	if err := store.Put(req); err != nil {
		t.Fatalf("seed requirement Put: %v", err)
	}

	first, err := Build(context.Background(), store, BuildInput{
		TaskID:                        "bd-task-3",
		RequirementID:                 req.Id,
		TaskScope:                     "Implement the thing",
		TaskAcceptanceCriteria:        []string{"It works"},
		TaskDefinitionOfDone:          "Merged",
		TaskExpectedFilesOrComponents: []string{"internal/thing/thing.go"},
		AffectedSymbolsAndFiles:       []string{"Thing.Do"},
		RequiredTests:                 []string{"TestThing_Do"},
	})
	if err != nil {
		t.Fatalf("first Build: %v", err)
	}

	// Resume: only RequiredTests actually changed for this round. Every
	// other field is omitted (zero value) and must be inherited from
	// Previous rather than coming back empty.
	second, err := Build(context.Background(), store, BuildInput{
		TaskID:        "bd-task-3",
		RequirementID: req.Id,
		RequiredTests: []string{"TestThing_Do", "TestThing_DoTwice"},
		Previous:      &first,
	})
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}

	if second.TaskDefinition.Scope != first.TaskDefinition.Scope {
		t.Errorf("Scope: got %q, want inherited %q", second.TaskDefinition.Scope, first.TaskDefinition.Scope)
	}
	if !equalStrings(second.TaskDefinition.AcceptanceCriteria, first.TaskDefinition.AcceptanceCriteria) {
		t.Errorf("AcceptanceCriteria: got %v, want inherited %v", second.TaskDefinition.AcceptanceCriteria, first.TaskDefinition.AcceptanceCriteria)
	}
	if second.TaskDefinition.DefinitionOfDone != first.TaskDefinition.DefinitionOfDone {
		t.Errorf("DefinitionOfDone: got %q, want inherited %q", second.TaskDefinition.DefinitionOfDone, first.TaskDefinition.DefinitionOfDone)
	}
	if !equalStrings(second.TaskDefinition.ExpectedFilesOrComponents, first.TaskDefinition.ExpectedFilesOrComponents) {
		t.Errorf("ExpectedFilesOrComponents: got %v, want inherited %v", second.TaskDefinition.ExpectedFilesOrComponents, first.TaskDefinition.ExpectedFilesOrComponents)
	}
	if !equalStrings(second.AffectedSymbolsAndFiles, first.AffectedSymbolsAndFiles) {
		t.Errorf("AffectedSymbolsAndFiles: got %v, want inherited %v", second.AffectedSymbolsAndFiles, first.AffectedSymbolsAndFiles)
	}
	if !equalStrings(second.RequiredTests, []string{"TestThing_Do", "TestThing_DoTwice"}) {
		t.Errorf("RequiredTests: got %v, want the explicitly-supplied value, not inherited", second.RequiredTests)
	}
}

func TestBuildExplicitFieldsOverridePrevious(t *testing.T) {
	store := newTestStore(t)
	req := baseRecord("pkw:requirement/test-ws/REQ-4", protocol.KnowledgeRecordTypeRequirement, "Override on resume")
	if err := store.Put(req); err != nil {
		t.Fatalf("seed requirement Put: %v", err)
	}

	first, err := Build(context.Background(), store, BuildInput{
		TaskID:        "bd-task-4",
		RequirementID: req.Id,
		TaskScope:     "Original scope",
	})
	if err != nil {
		t.Fatalf("first Build: %v", err)
	}

	second, err := Build(context.Background(), store, BuildInput{
		TaskID:        "bd-task-4",
		RequirementID: req.Id,
		TaskScope:     "Narrowed scope",
		Previous:      &first,
	})
	if err != nil {
		t.Fatalf("second Build: %v", err)
	}
	if second.TaskDefinition.Scope != "Narrowed scope" {
		t.Fatalf("Scope: got %q, want the explicitly-supplied value to win over Previous", second.TaskDefinition.Scope)
	}
}

func TestReadYAMLMissingFileReturnsNotFound(t *testing.T) {
	root := t.TempDir()
	_, found, err := ReadYAML(filepath.Join(root, "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("ReadYAML: %v", err)
	}
	if found {
		t.Fatal("found = true for a nonexistent file, want false")
	}
}

func TestReadFromBundleRoundTripsWriteToBundle(t *testing.T) {
	store := newTestStore(t)
	req := baseRecord("pkw:requirement/test-ws/REQ-5", protocol.KnowledgeRecordTypeRequirement, "Round trip via bundle")
	if err := store.Put(req); err != nil {
		t.Fatalf("seed requirement Put: %v", err)
	}

	built, err := Build(context.Background(), store, BuildInput{
		TaskID:        "bd-task-5",
		RequirementID: req.Id,
		TaskScope:     "Round trip scope",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	bundle, err := evidence.NewBundle(t.TempDir(), "run-1", "bd-task-5")
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}

	if _, found, err := ReadFromBundle(bundle); err != nil || found {
		t.Fatalf("ReadFromBundle before any write: found=%v err=%v, want found=false err=nil", found, err)
	}

	if err := WriteToBundle(built, bundle); err != nil {
		t.Fatalf("WriteToBundle: %v", err)
	}

	got, found, err := ReadFromBundle(bundle)
	if err != nil {
		t.Fatalf("ReadFromBundle: %v", err)
	}
	if !found {
		t.Fatal("found = false after WriteToBundle, want true")
	}
	if got.TaskDefinition.Scope != "Round trip scope" {
		t.Fatalf("Scope: got %q, want %q", got.TaskDefinition.Scope, "Round trip scope")
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
