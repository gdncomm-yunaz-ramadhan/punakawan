package recipe

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func newTestExecutor(t *testing.T, search JiraSearchClient) (*Executor, *Repository) {
	t.Helper()
	store := newTestStore(t)
	repo := &Repository{Store: store}
	resolver := &Resolver{Repo: repo}
	compiler := NewCompiler(nil)

	ledger, err := evidence.OpenLedger(t.TempDir(), "run-1")
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}

	return &Executor{Repo: repo, Resolver: resolver, Compiler: compiler, Search: search, Ledger: ledger}, repo
}

func withSelector(rec protocol.KnowledgeRecord) protocol.KnowledgeRecord {
	rec.RetrievalRecipe.Selector = protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{allEquals("project", literalValue("TRF"))},
	}
	return rec
}

func verifiedRecipeFixture(id string) protocol.KnowledgeRecord {
	return withSelector(recipeFixture{id: id, capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build())
}

func TestResolveAndExecuteRunsAVerifiedRecipe(t *testing.T) {
	search := &fakeSearch{issues: []ResultRow{{Key: "TRF-1", Summary: "a"}, {Key: "TRF-2", Summary: "b"}}}
	exec, repo := newTestExecutor(t, search)

	rec := verifiedRecipeFixture("pkw:recipe/a/only")
	if err := repo.Store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := exec.ResolveAndExecute(context.Background(), OperationRequest{Capability: "jira.issue.search"}, nil, "run-1", "task-1", time.Now())
	if err != nil {
		t.Fatalf("ResolveAndExecute: %v", err)
	}
	if got.RecipeID != rec.Id {
		t.Fatalf("RecipeID = %q, want %q", got.RecipeID, rec.Id)
	}
	if len(got.Issues) != 2 {
		t.Fatalf("Issues = %v, want 2", got.Issues)
	}

	updated, err := repo.Store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.RetrievalRecipe.LastExecution == nil {
		t.Fatal("LastExecution is nil, want it recorded after execution")
	}
	if updated.RetrievalRecipe.LastExecution.ResultCount == nil || *updated.RetrievalRecipe.LastExecution.ResultCount != 2 {
		t.Fatalf("LastExecution.ResultCount = %v, want 2", updated.RetrievalRecipe.LastExecution.ResultCount)
	}
}

func TestResolveAndExecuteReturnsDiscoveryNeededWhenNoCandidate(t *testing.T) {
	exec, _ := newTestExecutor(t, &fakeSearch{})

	_, err := exec.ResolveAndExecute(context.Background(), OperationRequest{Capability: "jira.issue.search"}, nil, "run-1", "task-1", time.Now())
	var need *DiscoveryNeededError
	if !errors.As(err, &need) {
		t.Fatalf("err = %v, want *DiscoveryNeededError", err)
	}
	if need.Outcome != OutcomeNotFound {
		t.Fatalf("need.Outcome = %q, want not_found", need.Outcome)
	}
}

func TestResolveAndExecuteRevalidatesStaleRecipeBeforeReuse(t *testing.T) {
	search := &fakeSearch{issues: []ResultRow{{Key: "TRF-1", Summary: "a"}}}
	exec, repo := newTestExecutor(t, search)

	rec := withSelector(recipeFixture{id: "pkw:recipe/a/aging", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateStale}.build())
	if err := repo.Store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := exec.ResolveAndExecute(context.Background(), OperationRequest{Capability: "jira.issue.search"}, nil, "run-1", "task-1", time.Now())
	if err != nil {
		t.Fatalf("ResolveAndExecute: %v", err)
	}
	if got.RecipeID != rec.Id {
		t.Fatalf("RecipeID = %q, want %q", got.RecipeID, rec.Id)
	}

	updated, err := repo.Store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Validity.State != protocol.KnowledgeRecordValidityStateVerified {
		t.Fatalf("Validity.State = %q, want verified after successful revalidation", updated.Validity.State)
	}
}

func TestResolveAndExecuteFailsWhenRevalidationFindsNothing(t *testing.T) {
	exec, repo := newTestExecutor(t, &fakeSearch{issues: nil})

	rec := withSelector(recipeFixture{id: "pkw:recipe/a/aging", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateStale}.build())
	if err := repo.Store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := exec.ResolveAndExecute(context.Background(), OperationRequest{Capability: "jira.issue.search"}, nil, "run-1", "task-1", time.Now()); err == nil {
		t.Fatal("ResolveAndExecute: want an error when revalidation finds zero results, got nil")
	}

	updated, err := repo.Store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Validity.State != protocol.KnowledgeRecordValidityStateStale {
		t.Fatalf("Validity.State = %q, want it to remain stale after a failed revalidation", updated.Validity.State)
	}
}

func TestResolveAndExecuteRecordsExecutionEvidence(t *testing.T) {
	search := &fakeSearch{issues: []ResultRow{{Key: "TRF-1", Summary: "a"}}}
	exec, repo := newTestExecutor(t, search)

	rec := verifiedRecipeFixture("pkw:recipe/a/only")
	if err := repo.Store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := exec.ResolveAndExecute(context.Background(), OperationRequest{Capability: "jira.issue.search"}, nil, "run-1", "task-1", time.Now())
	if err != nil {
		t.Fatalf("ResolveAndExecute: %v", err)
	}

	all, err := exec.Ledger.ForTask("task-1")
	if err != nil {
		t.Fatalf("ForTask: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ForTask returned %d records, want 1", len(all))
	}
	if all[0].Id != got.Evidence.Id {
		t.Fatalf("ledger record id = %q, want %q", all[0].Id, got.Evidence.Id)
	}
}
