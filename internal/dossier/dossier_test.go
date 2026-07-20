package dossier

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/internal/workspace"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// runGit is test fixture setup, not the code under test: it shells out
// directly via os/exec to build a real repository, mirroring
// internal/gitops/inspect_test.go's helper of the same name.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// initTestRepo creates a real git repository at dir with one commit and one
// uncommitted, tracked-file modification, mirroring
// internal/gitops/inspect_test.go's newTestRepo helper (adapted to take an
// explicit dir rather than allocating its own temp dir, since these repos
// need to live at specific paths under a shared workspace root).
func initTestRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}

	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")

	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "first commit")

	if err := os.WriteFile(readme, []byte("hello again\n"), 0o644); err != nil {
		t.Fatalf("modify README.md: %v", err)
	}
}

// newTestWorkspace builds a *workspace.Workspace whose root is a fresh temp
// dir containing two declared repositories, each a real git repo created
// directly at its final path (repo-a, repo-b, both directly under root so
// their workspace.Repository.Path is just their own directory name).
func newTestWorkspace(t *testing.T) (*workspace.Workspace, *tools.Supervisor) {
	t.Helper()
	root := t.TempDir()

	repoA := filepath.Join(root, "repo-a")
	repoB := filepath.Join(root, "repo-b")
	initTestRepo(t, repoA)
	initTestRepo(t, repoB)

	ws := &workspace.Workspace{
		Version: workspace.SupportedVersion,
		ID:      "test-ws",
		Name:    "Test Workspace",
		Root:    root,
		Repositories: []workspace.Repository{
			{ID: "repo-a", Path: "repo-a"},
			{ID: "repo-b", Path: "repo-b"},
		},
	}

	sup := tools.New(repoA, repoB)
	return ws, sup
}

// newTestStore mirrors internal/knowledge's own newTestStore helper,
// skipping if dolt is not installed.
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

func TestBuildProducesValidRecord(t *testing.T) {
	ws, sup := newTestWorkspace(t)
	store := newTestStore(t)

	in := BuildInput{
		WorkspaceID:         ws.ID,
		RunID:               "run-1",
		UserGoal:            "Let refunds settle same-day",
		BusinessOrUserValue: "Reduces support tickets",
		CurrentBehavior:     "Refunds settle next business day",
		DesiredBehavior:     "Refunds settle same day",
		ExplicitNonGoals:    []string{"Do not change the refund approval flow"},
		Assumptions:         []string{"Payment gateway supports same-day settlement"},
		MissingInformation:  []string{"Gateway rate limits"},
		Contradictions:      []string{"Ticket says T+1, doc says T+0"},
		ConfidenceLevel:     "medium",
	}

	rec, err := Build(context.Background(), ws, sup, store, in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if err := knowledge.Validate(rec); err != nil {
		t.Fatalf("Validate(rec): %v", err)
	}

	wantID := "pkw:contextdossier/" + ws.ID + "/run-1"
	if rec.Id != wantID {
		t.Fatalf("Id: got %q, want %q", rec.Id, wantID)
	}
	if rec.Type != protocol.KnowledgeRecordTypeContextDossier {
		t.Fatalf("Type: got %q, want %q", rec.Type, protocol.KnowledgeRecordTypeContextDossier)
	}
	if rec.Title != in.UserGoal {
		t.Fatalf("Title: got %q, want %q", rec.Title, in.UserGoal)
	}
	if rec.Source.Provider != "punakawan" {
		t.Fatalf("Source.Provider: got %q, want %q", rec.Source.Provider, "punakawan")
	}
	if rec.Source.RetrievedAt.IsZero() {
		t.Fatal("Source.RetrievedAt: got zero value")
	}
	if rec.Extraction.Method != protocol.KnowledgeRecordExtractionMethodModelAssisted {
		t.Fatalf("Extraction.Method: got %q, want %q", rec.Extraction.Method, protocol.KnowledgeRecordExtractionMethodModelAssisted)
	}
	if rec.Validity.State != protocol.KnowledgeRecordValidityStateObserved {
		t.Fatalf("Validity.State: got %q, want %q", rec.Validity.State, protocol.KnowledgeRecordValidityStateObserved)
	}

	cd := rec.ContextDossier
	if cd == nil {
		t.Fatal("ContextDossier: got nil")
	}

	// Caller-supplied intent fields must pass through unchanged.
	if cd.UserGoal == nil || *cd.UserGoal != in.UserGoal {
		t.Fatalf("UserGoal: got %v, want %q", cd.UserGoal, in.UserGoal)
	}
	if cd.BusinessOrUserValue == nil || *cd.BusinessOrUserValue != in.BusinessOrUserValue {
		t.Fatalf("BusinessOrUserValue: got %v, want %q", cd.BusinessOrUserValue, in.BusinessOrUserValue)
	}
	if cd.CurrentBehavior == nil || *cd.CurrentBehavior != in.CurrentBehavior {
		t.Fatalf("CurrentBehavior: got %v, want %q", cd.CurrentBehavior, in.CurrentBehavior)
	}
	if cd.DesiredBehavior == nil || *cd.DesiredBehavior != in.DesiredBehavior {
		t.Fatalf("DesiredBehavior: got %v, want %q", cd.DesiredBehavior, in.DesiredBehavior)
	}
	if cd.ConfidenceLevel == nil || *cd.ConfidenceLevel != in.ConfidenceLevel {
		t.Fatalf("ConfidenceLevel: got %v, want %q", cd.ConfidenceLevel, in.ConfidenceLevel)
	}
	if !equalStrings(cd.ExplicitNonGoals, in.ExplicitNonGoals) {
		t.Fatalf("ExplicitNonGoals: got %v, want %v", cd.ExplicitNonGoals, in.ExplicitNonGoals)
	}
	if !equalStrings(cd.Assumptions, in.Assumptions) {
		t.Fatalf("Assumptions: got %v, want %v", cd.Assumptions, in.Assumptions)
	}
	if !equalStrings(cd.MissingInformation, in.MissingInformation) {
		t.Fatalf("MissingInformation: got %v, want %v", cd.MissingInformation, in.MissingInformation)
	}
	if !equalStrings(cd.Contradictions, in.Contradictions) {
		t.Fatalf("Contradictions: got %v, want %v", cd.Contradictions, in.Contradictions)
	}

	// Defaulting to every workspace repository when none specified.
	if !equalStrings(cd.AffectedRepositories, []string{"repo-a", "repo-b"}) {
		t.Fatalf("AffectedRepositories: got %v, want [repo-a repo-b]", cd.AffectedRepositories)
	}
	if len(cd.SourceInventory) != 2 {
		t.Fatalf("SourceInventory: got %v, want 2 entries", cd.SourceInventory)
	}
	// Each test repo has one uncommitted, tracked-file modification
	// (README.md), so ExistingImplementationPaths should reflect both.
	if len(cd.ExistingImplementationPaths) != 2 {
		t.Fatalf("ExistingImplementationPaths: got %v, want 2 entries", cd.ExistingImplementationPaths)
	}

	// Left empty deliberately: no clean signal to populate these yet.
	if len(cd.ExistingTests) != 0 {
		t.Fatalf("ExistingTests: got %v, want empty", cd.ExistingTests)
	}
	if cd.DeploymentPath != nil {
		t.Fatalf("DeploymentPath: got %v, want nil", cd.DeploymentPath)
	}
}

func TestBuildRespectsExplicitAffectedRepositories(t *testing.T) {
	ws, sup := newTestWorkspace(t)
	store := newTestStore(t)

	in := BuildInput{
		WorkspaceID:          ws.ID,
		RunID:                "run-2",
		AffectedRepositories: []string{"repo-b"},
	}

	rec, err := Build(context.Background(), ws, sup, store, in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := knowledge.Validate(rec); err != nil {
		t.Fatalf("Validate(rec): %v", err)
	}

	cd := rec.ContextDossier
	if !equalStrings(cd.AffectedRepositories, []string{"repo-b"}) {
		t.Fatalf("AffectedRepositories: got %v, want [repo-b]", cd.AffectedRepositories)
	}
	if len(cd.SourceInventory) != 1 {
		t.Fatalf("SourceInventory: got %v, want 1 entry", cd.SourceInventory)
	}
}

func TestBuildFallsBackTitleToRunID(t *testing.T) {
	ws, sup := newTestWorkspace(t)
	store := newTestStore(t)

	in := BuildInput{WorkspaceID: ws.ID, RunID: "run-3"}
	rec, err := Build(context.Background(), ws, sup, store, in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := knowledge.Validate(rec); err != nil {
		t.Fatalf("Validate(rec): %v", err)
	}
	want := "context dossier for run run-3"
	if rec.Title != want {
		t.Fatalf("Title: got %q, want %q", rec.Title, want)
	}
}

func TestBuildRejectsUnknownRepository(t *testing.T) {
	ws, sup := newTestWorkspace(t)
	store := newTestStore(t)

	in := BuildInput{
		WorkspaceID:          ws.ID,
		RunID:                "run-4",
		AffectedRepositories: []string{"does-not-exist"},
	}
	if _, err := Build(context.Background(), ws, sup, store, in); err == nil {
		t.Fatal("Build: expected error for an unknown repository id")
	}
}

func TestBuildPullsInPreviousDecisions(t *testing.T) {
	ws, sup := newTestWorkspace(t)
	store := newTestStore(t)

	decision := protocol.KnowledgeRecord{
		Id:     "pkw:decision/" + ws.ID + "/DEC-1",
		Type:   protocol.KnowledgeRecordTypeDecision,
		Status: "active",
		Title:  "Use same-day settlement via gateway webhook",
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
	if err := store.Put(decision); err != nil {
		t.Fatalf("seed decision Put: %v", err)
	}

	in := BuildInput{WorkspaceID: ws.ID, RunID: "run-5"}
	rec, err := Build(context.Background(), ws, sup, store, in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := knowledge.Validate(rec); err != nil {
		t.Fatalf("Validate(rec): %v", err)
	}

	found := false
	for _, d := range rec.ContextDossier.RelevantPreviousDecisions {
		if d == decision.Title+" ("+decision.Id+")" {
			found = true
		}
	}
	if !found {
		t.Fatalf("RelevantPreviousDecisions: got %v, want an entry for %q", rec.ContextDossier.RelevantPreviousDecisions, decision.Id)
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
