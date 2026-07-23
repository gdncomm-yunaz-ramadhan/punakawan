package recipe

import (
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestResolverReturnsResolvedForOneClearVerifiedCandidate(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}
	resolver := &Resolver{Repo: repo}

	only := recipeFixture{
		id:           "pkw:recipe/a/only",
		capability:   "jira.issue.search",
		intent:       "project.next-sprint.issues",
		workspaceIDs: []string{"affiliate-platform"},
		state:        protocol.KnowledgeRecordValidityStateVerified,
	}.build()
	if err := store.Put(only); err != nil {
		t.Fatalf("Put: %v", err)
	}

	res, err := resolver.Resolve(OperationRequest{Capability: "jira.issue.search", Intent: "project.next-sprint.issues", WorkspaceID: "affiliate-platform"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeResolved {
		t.Fatalf("Outcome = %q, want resolved", res.Outcome)
	}
	if res.Selected == nil || res.Selected.Record.Id != only.Id {
		t.Fatalf("Selected = %+v, want %q", res.Selected, only.Id)
	}
	if len(res.Selected.Explanation) == 0 {
		t.Fatal("Explanation is empty, want at least one scored signal")
	}
}

func TestResolverReturnsNotFoundWhenNoCandidateExists(t *testing.T) {
	store := newTestStore(t)
	resolver := &Resolver{Repo: &Repository{Store: store}}

	res, err := resolver.Resolve(OperationRequest{Capability: "jira.issue.search", WorkspaceID: "affiliate-platform"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeNotFound {
		t.Fatalf("Outcome = %q, want not_found", res.Outcome)
	}
}

func TestResolverReturnsStaleForBestStaleCandidate(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}
	resolver := &Resolver{Repo: repo}

	rec := recipeFixture{
		id:           "pkw:recipe/a/aging",
		capability:   "jira.issue.search",
		workspaceIDs: []string{"affiliate-platform"},
		state:        protocol.KnowledgeRecordValidityStateStale,
	}.build()
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	res, err := resolver.Resolve(OperationRequest{Capability: "jira.issue.search", WorkspaceID: "affiliate-platform"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeStale {
		t.Fatalf("Outcome = %q, want stale", res.Outcome)
	}
	if res.Selected == nil || res.Selected.Record.Id != rec.Id {
		t.Fatalf("Selected = %+v, want %q", res.Selected, rec.Id)
	}
}

func TestResolverReturnsAmbiguousForMaterallyTiedCandidates(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}
	resolver := &Resolver{Repo: repo}

	// Neither declares a workspace scope (both globally scoped, same
	// base score) and neither matches the request's intent or repo, so
	// they tie exactly - a genuinely ambiguous pair, not just two
	// candidates that happen to both exist.
	a := recipeFixture{id: "pkw:recipe/a/candidate-a", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	b := recipeFixture{id: "pkw:recipe/a/candidate-b", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	if err := store.Put(a); err != nil {
		t.Fatalf("Put(a): %v", err)
	}
	if err := store.Put(b); err != nil {
		t.Fatalf("Put(b): %v", err)
	}

	res, err := resolver.Resolve(OperationRequest{Capability: "jira.issue.search", WorkspaceID: "unrelated-workspace"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeAmbiguous {
		t.Fatalf("Outcome = %q, want ambiguous", res.Outcome)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("Candidates = %+v, want 2", res.Candidates)
	}
	if res.Selected != nil {
		t.Fatalf("Selected = %+v, want nil for an ambiguous outcome", res.Selected)
	}
}

func TestResolverPrefersExactWorkspaceScopeOverGlobal(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}
	resolver := &Resolver{Repo: repo}

	global := recipeFixture{id: "pkw:recipe/a/global", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	scoped := recipeFixture{
		id:           "pkw:recipe/a/scoped",
		capability:   "jira.issue.search",
		workspaceIDs: []string{"affiliate-platform"},
		state:        protocol.KnowledgeRecordValidityStateVerified,
	}.build()
	if err := store.Put(global); err != nil {
		t.Fatalf("Put(global): %v", err)
	}
	if err := store.Put(scoped); err != nil {
		t.Fatalf("Put(scoped): %v", err)
	}

	res, err := resolver.Resolve(OperationRequest{Capability: "jira.issue.search", WorkspaceID: "affiliate-platform"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Outcome != OutcomeResolved {
		t.Fatalf("Outcome = %q, want resolved", res.Outcome)
	}
	if res.Selected.Record.Id != scoped.Id {
		t.Fatalf("Selected = %q, want the exactly-scoped recipe %q", res.Selected.Record.Id, scoped.Id)
	}
}
