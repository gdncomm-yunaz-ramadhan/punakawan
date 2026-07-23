package recipe

import (
	"context"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// TestResolveAndExecuteRevalidatesOnInstanceFingerprintMismatch is task
// q9r.7 #1's end-to-end check: a verified recipe validated against one
// Jira instance must not be silently reused against a different one, even
// though nothing about its capability/scope/selector looks stale.
func TestResolveAndExecuteRevalidatesOnInstanceFingerprintMismatch(t *testing.T) {
	search := &fakeSearch{issues: []JiraIssue{{Key: "TRF-1", Summary: "a"}}}
	exec, repo := newTestExecutor(t, search)
	exec.Instance = InstanceFingerprint{Host: "company-b.atlassian.net", CloudID: "cloud-b"}

	rec := verifiedRecipeFixture("pkw:recipe/a/wrong-instance")
	staleFP := (InstanceFingerprint{Host: "company-a.atlassian.net", CloudID: "cloud-a"}).String()
	rec.RetrievalRecipe.Validation = &protocol.KnowledgeRecordRetrievalRecipeValidation{ProviderInstanceFingerprint: &staleFP}
	if err := repo.Store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := exec.ResolveAndExecute(context.Background(), OperationRequest{Capability: "jira.issue.search"}, nil, "run-1", "task-1", time.Now())
	if err != nil {
		t.Fatalf("ResolveAndExecute: %v, want it to revalidate through the mismatch and still succeed", err)
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
	if updated.RetrievalRecipe.Validation == nil || updated.RetrievalRecipe.Validation.ProviderInstanceFingerprint == nil {
		t.Fatal("expected the current instance fingerprint to be stamped after revalidation")
	}
	if got := *updated.RetrievalRecipe.Validation.ProviderInstanceFingerprint; got != exec.Instance.String() {
		t.Fatalf("stamped fingerprint = %q, want %q", got, exec.Instance.String())
	}
}

// TestResolveAndExecuteDoesNotRevalidateOnMatchingInstance is the negative
// case: a matching fingerprint must not force an unnecessary revalidation
// (which would otherwise defeat the "one clear verified candidate: reuse
// automatically" fast path, §7).
func TestResolveAndExecuteDoesNotRevalidateOnMatchingInstance(t *testing.T) {
	search := &fakeSearch{issues: []JiraIssue{{Key: "TRF-1", Summary: "a"}}}
	exec, repo := newTestExecutor(t, search)
	fp := InstanceFingerprint{Host: "company-a.atlassian.net", CloudID: "cloud-a"}
	exec.Instance = fp

	rec := verifiedRecipeFixture("pkw:recipe/a/same-instance")
	fpStr := fp.String()
	rec.RetrievalRecipe.Validation = &protocol.KnowledgeRecordRetrievalRecipeValidation{ProviderInstanceFingerprint: &fpStr}
	if err := repo.Store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := exec.ResolveAndExecute(context.Background(), OperationRequest{Capability: "jira.issue.search"}, nil, "run-1", "task-1", time.Now()); err != nil {
		t.Fatalf("ResolveAndExecute: %v", err)
	}

	if search.calls != 1 {
		t.Fatalf("Search called %d times, want exactly 1 (no extra revalidation dry run)", search.calls)
	}
}

// TestResolveAndExecuteMarksRecipeStaleOnProviderRejection is task q9r.7
// #3's schema-change-detection check: a live Jira rejection of a
// previously-fine query (the closest signal this package can observe
// without a dedicated field-metadata integration - see this package's doc
// comment on why a full custom-field-schema fetch is out of scope) must
// not be silently swallowed as a one-off failure; it must move the recipe
// to stale so the next call is forced through revalidation instead of
// repeating the same broken query forever.
func TestResolveAndExecuteMarksRecipeStaleOnProviderRejection(t *testing.T) {
	search := &fakeSearch{err: errRejected("field no longer exists")}
	exec, repo := newTestExecutor(t, search)

	rec := verifiedRecipeFixture("pkw:recipe/a/schema-drifted")
	if err := repo.Store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := exec.ResolveAndExecute(context.Background(), OperationRequest{Capability: "jira.issue.search"}, nil, "run-1", "task-1", time.Now()); err == nil {
		t.Fatal("ResolveAndExecute: want an error when the provider rejects the compiled query")
	}

	updated, err := repo.Store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Validity.State != protocol.KnowledgeRecordValidityStateStale {
		t.Fatalf("Validity.State = %q, want stale after a provider rejection", updated.Validity.State)
	}

	// The very next resolve must go through revalidation rather than
	// immediately re-attempting (and re-failing) the same query.
	search.err = nil
	search.issues = []JiraIssue{{Key: "TRF-9", Summary: "fixed"}}
	got, err := exec.ResolveAndExecute(context.Background(), OperationRequest{Capability: "jira.issue.search"}, nil, "run-1", "task-2", time.Now())
	if err != nil {
		t.Fatalf("ResolveAndExecute after fix: %v", err)
	}
	if got.RecipeID != rec.Id {
		t.Fatalf("RecipeID = %q, want %q", got.RecipeID, rec.Id)
	}
}

type errRejected string

func (e errRejected) Error() string { return string(e) }
