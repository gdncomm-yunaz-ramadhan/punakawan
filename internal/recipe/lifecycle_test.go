package recipe

import (
	"context"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestDisputePreventsAutomaticReuse(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}

	rec := recipeFixture{id: "pkw:recipe/a/disputable", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := repo.Dispute(rec.Id, "returned wrong issues"); err != nil {
		t.Fatalf("Dispute: %v", err)
	}

	got, err := repo.Search(Query{Capability: "jira.issue.search"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range got {
		if r.Id == rec.Id {
			t.Fatalf("Search still returned disputed %q", rec.Id)
		}
	}
}

func TestSupersedeLinksToReplacement(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}

	old := recipeFixture{id: "pkw:recipe/a/old", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	replacement := recipeFixture{id: "pkw:recipe/a/replacement", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	if err := store.Put(old); err != nil {
		t.Fatalf("Put(old): %v", err)
	}
	if err := store.Put(replacement); err != nil {
		t.Fatalf("Put(replacement): %v", err)
	}

	if err := repo.Supersede(old.Id, replacement.Id); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	got, err := store.Get(old.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SupersededBy == nil || *got.SupersededBy != replacement.Id {
		t.Fatalf("SupersededBy = %v, want %q", got.SupersededBy, replacement.Id)
	}
	if got.Validity.State != protocol.KnowledgeRecordValidityStateSuperseded {
		t.Fatalf("Validity.State = %q, want superseded", got.Validity.State)
	}
}

func TestBeginUpdateMovesToValidatingAndExcludesFromSearch(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}

	rec := recipeFixture{id: "pkw:recipe/a/editable", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	baseline, err := repo.BeginUpdate(rec.Id)
	if err != nil {
		t.Fatalf("BeginUpdate: %v", err)
	}
	if baseline.RetrievalRecipe.Capability != rec.RetrievalRecipe.Capability {
		t.Fatalf("baseline capability = %q, want %q", baseline.RetrievalRecipe.Capability, rec.RetrievalRecipe.Capability)
	}

	got, err := repo.Search(Query{Capability: "jira.issue.search"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range got {
		if r.Id == rec.Id {
			t.Fatalf("Search returned a validating (mid-update) recipe %q", rec.Id)
		}
	}
}

func TestCompileOnlyValidateDoesNotRequireASearchClient(t *testing.T) {
	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{allEquals("project", literalValue("TRF"))},
	})
	cq, err := CompileOnlyValidate(context.Background(), NewCompiler(nil), rr, nil)
	if err != nil {
		t.Fatalf("CompileOnlyValidate: %v", err)
	}
	if cq.JQL != `project = "TRF"` {
		t.Fatalf("JQL = %q, want project = \"TRF\"", cq.JQL)
	}
}

func TestRevalidationDue(t *testing.T) {
	acceptedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rec := protocol.KnowledgeRecord{
		RetrievalRecipe: &protocol.KnowledgeRecordRetrievalRecipe{
			Validation: &protocol.KnowledgeRecordRetrievalRecipeValidation{AcceptedAt: &acceptedAt},
		},
	}

	now := acceptedAt.Add(31 * 24 * time.Hour)
	if !RevalidationDue(rec, 30*24*time.Hour, now) {
		t.Fatal("RevalidationDue = false, want true after the period has elapsed")
	}
	if RevalidationDue(rec, 30*24*time.Hour, acceptedAt.Add(24*time.Hour)) {
		t.Fatal("RevalidationDue = true, want false before the period elapses")
	}
	if RevalidationDue(rec, 0, now) {
		t.Fatal("RevalidationDue = true, want false when no period is configured")
	}
}
