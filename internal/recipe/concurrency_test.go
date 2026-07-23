package recipe

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// TestConcurrentResolveAndExecuteAgainstStaleRecipeDoesNotRace is task
// q9r.7 #5's concurrency check: "does Executor.ResolveAndExecute handle
// concurrent revalidation-then-execute races safely - e.g. two callers
// both see a stale recipe, both trigger revalidation, do they double-write
// or race?"
//
// Executor has no in-process locking of its own (no mutex over a recipe
// id, no compare-and-swap on Store.Put) - Repository.Verify/Store.Put is a
// plain upsert. This test's job is to confirm that lack of locking is
// merely "last write wins" (an acceptable, if slightly wasteful, outcome:
// both callers redundantly revalidate and both successfully execute) and
// not a data race or a corrupted record - run with -race, which is what
// would actually catch the latter.
func TestConcurrentResolveAndExecuteAgainstStaleRecipeDoesNotRace(t *testing.T) {
	const goroutines = 8

	search := &countingSearch{issues: []JiraIssue{{Key: "TRF-1", Summary: "a"}}}
	exec, repo := newTestExecutor(t, search)

	rec := withSelector(recipeFixture{id: "pkw:recipe/a/racey", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateStale}.build())
	if err := repo.Store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := exec.ResolveAndExecute(context.Background(), OperationRequest{Capability: "jira.issue.search"}, nil, "run-1", "task-race", time.Now())
			errCh <- err
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("concurrent ResolveAndExecute: %v", err)
		}
	}

	// The record must still be readable and structurally intact - not
	// half-written by two overlapping Puts - and end up verified, not
	// stuck in some intermediate state.
	final, err := repo.Store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get after concurrent execution: %v", err)
	}
	if final.Validity.State != protocol.KnowledgeRecordValidityStateVerified {
		t.Fatalf("Validity.State = %q, want verified after concurrent revalidation settles", final.Validity.State)
	}
	if final.RetrievalRecipe.LastExecution == nil {
		t.Fatal("LastExecution is nil after concurrent execution, want the last writer's result recorded")
	}

	// Every goroutine that reached the provider should have logged its
	// own execution evidence - concurrent Ledger.Append calls (append-only
	// file writes) must not lose or corrupt entries either.
	all, err := exec.Ledger.ForTask("task-race")
	if err != nil {
		t.Fatalf("ForTask: %v", err)
	}
	// Each goroutine revalidates (1 Search call) then executes (1 Search
	// call) = 2 calls each; only the execute half logs execution
	// evidence, so we expect exactly `goroutines` evidence records.
	if len(all) != goroutines {
		t.Fatalf("evidence count = %d, want %d (one execution record per goroutine, none lost or duplicated)", len(all), goroutines)
	}
	if search.calls.Load() != int64(goroutines)*2 {
		t.Fatalf("search.calls = %d, want %d (revalidate+execute per goroutine)", search.calls.Load(), goroutines*2)
	}
}

// countingSearch is a JiraSearchClient safe for concurrent use, unlike
// fakeSearch (whose issues/err fields are read-only here so a data race on
// them isn't a concern, but calls needs atomic counting under -race).
type countingSearch struct {
	issues []JiraIssue
	calls  atomic.Int64
}

func (c *countingSearch) Search(ctx context.Context, jql, orderBy string, fields []string, maxResults int) ([]JiraIssue, error) {
	c.calls.Add(1)
	return c.issues, nil
}

// TestConcurrentResolveAndExecuteAgainstVerifiedRecipeDoesNotRace covers
// the more common case: many concurrent reuses of an already-verified
// recipe (no revalidation involved), which should run entirely without
// shared mutable state races since each call reloads/writes the record
// independently.
func TestConcurrentResolveAndExecuteAgainstVerifiedRecipeDoesNotRace(t *testing.T) {
	const goroutines = 8

	search := &countingSearch{issues: []JiraIssue{{Key: "TRF-1", Summary: "a"}}}
	exec, repo := newTestExecutor(t, search)

	rec := verifiedRecipeFixture("pkw:recipe/a/verified-racey")
	if err := repo.Store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := exec.ResolveAndExecute(context.Background(), OperationRequest{Capability: "jira.issue.search"}, nil, "run-1", "task-verified-race", time.Now()); err != nil {
				t.Errorf("ResolveAndExecute: %v", err)
			}
		}()
	}
	wg.Wait()

	if search.calls.Load() != int64(goroutines) {
		t.Fatalf("search.calls = %d, want %d (one execute call per goroutine, no revalidation)", search.calls.Load(), goroutines)
	}
}
