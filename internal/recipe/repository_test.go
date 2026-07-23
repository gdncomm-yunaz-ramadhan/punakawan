package recipe

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

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

// recipeFixture is a minimal, parameterized retrieval-recipe record for
// repository/resolver tests. workspaceIDs/repoIDs may be nil for an
// unscoped (globally applicable) recipe.
type recipeFixture struct {
	id           string
	capability   string
	intent       string
	aliases      []string
	workspaceIDs []string
	repoIDs      []string
	state        protocol.KnowledgeRecordValidityState
	priorSuccess bool
}

func (f recipeFixture) build() protocol.KnowledgeRecord {
	now := time.Now().UTC()
	rr := &protocol.KnowledgeRecordRetrievalRecipe{
		Capability: f.capability,
		Intent:     f.intent,
		Provider:   "jira",
		Resource:   "issue",
		Operation:  "search",
		ReadOnly:   true,
		Selector:   protocol.KnowledgeRecordRetrievalRecipeSelector{},
		Output: protocol.KnowledgeRecordRetrievalRecipeOutput{
			EntityType:    "jira_issue",
			IdentityField: "key",
			Fields:        []string{"key"},
		},
	}
	if len(f.workspaceIDs) > 0 || len(f.repoIDs) > 0 {
		rr.AppliesTo = &protocol.KnowledgeRecordRetrievalRecipeAppliesTo{
			WorkspaceIds:  f.workspaceIDs,
			RepositoryIds: f.repoIDs,
		}
	}
	if f.priorSuccess {
		status := protocol.KnowledgeRecordRetrievalRecipeLastExecutionStatusSuccess
		rr.LastExecution = &protocol.KnowledgeRecordRetrievalRecipeLastExecution{Status: &status}
	}

	return protocol.KnowledgeRecord{
		Id:      f.id,
		Type:    protocol.KnowledgeRecordTypeRetrievalRecipe,
		Status:  "active",
		Title:   "Find work items",
		Aliases: f.aliases,
		Source: protocol.KnowledgeRecordSource{
			Provider:    "user_instruction",
			RetrievedAt: now,
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodManual,
		},
		Validity:        validity(f.state),
		RetrievalRecipe: rr,
	}
}

// validity builds a Validity satisfying Store.Put's own consistency
// checks (§7.4: a verified state requires a non-empty verified_by).
func validity(state protocol.KnowledgeRecordValidityState) protocol.KnowledgeRecordValidity {
	v := protocol.KnowledgeRecordValidity{State: state}
	if state == protocol.KnowledgeRecordValidityStateVerified {
		v.VerifiedBy = []string{"user"}
	}
	return v
}

func TestRepositorySearchExcludesUnreusableStates(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}

	fixtures := []recipeFixture{
		{id: "pkw:recipe/a/verified", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified},
		{id: "pkw:recipe/a/stale", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateStale},
		{id: "pkw:recipe/a/draft", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateDraft},
		{id: "pkw:recipe/a/validating", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateValidating},
		{id: "pkw:recipe/a/disputed", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateDisputed},
		{id: "pkw:recipe/a/superseded", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateSuperseded},
		{id: "pkw:recipe/a/invalid", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateInvalid},
	}
	for _, f := range fixtures {
		if err := store.Put(f.build()); err != nil {
			t.Fatalf("Put(%s): %v", f.id, err)
		}
	}

	got, err := repo.Search(Query{Capability: "jira.issue.search"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Search returned %d candidates, want 2 (verified, stale): %+v", len(got), got)
	}
	ids := map[string]bool{got[0].Id: true, got[1].Id: true}
	if !ids["pkw:recipe/a/verified"] || !ids["pkw:recipe/a/stale"] {
		t.Fatalf("Search returned %v, want verified+stale only", ids)
	}
}

func TestRepositorySearchFiltersByCapabilityAndWorkspaceScope(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}

	recs := []recipeFixture{
		{id: "pkw:recipe/a/wrong-capability", capability: "confluence.page.search", state: protocol.KnowledgeRecordValidityStateVerified},
		{id: "pkw:recipe/a/scoped-elsewhere", capability: "jira.issue.search", workspaceIDs: []string{"other-workspace"}, state: protocol.KnowledgeRecordValidityStateVerified},
		{id: "pkw:recipe/a/scoped-here", capability: "jira.issue.search", workspaceIDs: []string{"affiliate-platform"}, state: protocol.KnowledgeRecordValidityStateVerified},
		{id: "pkw:recipe/a/global", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified},
	}
	for _, f := range recs {
		if err := store.Put(f.build()); err != nil {
			t.Fatalf("Put(%s): %v", f.id, err)
		}
	}

	got, err := repo.Search(Query{Capability: "jira.issue.search", WorkspaceID: "affiliate-platform"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Search returned %d candidates, want 2 (scoped-here, global): %+v", len(got), got)
	}
	for _, rec := range got {
		if rec.Id == "pkw:recipe/a/wrong-capability" || rec.Id == "pkw:recipe/a/scoped-elsewhere" {
			t.Fatalf("Search incorrectly returned %q", rec.Id)
		}
	}
}

func TestRepositoryCreateVersionSupersedesPrevious(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}

	v1 := recipeFixture{id: "pkw:recipe/a/lineage@1", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	if _, err := repo.CreateVersion(v1, ""); err != nil {
		t.Fatalf("CreateVersion(v1): %v", err)
	}

	v2 := recipeFixture{id: "pkw:recipe/a/lineage@2", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	if _, err := repo.CreateVersion(v2, v1.Id); err != nil {
		t.Fatalf("CreateVersion(v2): %v", err)
	}

	oldRec, err := store.Get(v1.Id)
	if err != nil {
		t.Fatalf("Get(v1): %v", err)
	}
	if oldRec.SupersededBy == nil || *oldRec.SupersededBy != v2.Id {
		t.Fatalf("v1.SupersededBy = %v, want %q", oldRec.SupersededBy, v2.Id)
	}
	if oldRec.Validity.State != protocol.KnowledgeRecordValidityStateSuperseded {
		t.Fatalf("v1.Validity.State = %q, want superseded", oldRec.Validity.State)
	}

	// The superseded v1 must not appear in Search results any more.
	got, err := repo.Search(Query{Capability: "jira.issue.search"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, rec := range got {
		if rec.Id == v1.Id {
			t.Fatalf("Search still returned superseded %q", v1.Id)
		}
	}
}

func TestRepositoryMarkState(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}

	rec := recipeFixture{id: "pkw:recipe/a/disputable", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := repo.MarkState(rec.Id, protocol.KnowledgeRecordValidityStateDisputed, "user reported wrong results"); err != nil {
		t.Fatalf("MarkState: %v", err)
	}

	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Validity.State != protocol.KnowledgeRecordValidityStateDisputed {
		t.Fatalf("Validity.State = %q, want disputed", got.Validity.State)
	}
}
