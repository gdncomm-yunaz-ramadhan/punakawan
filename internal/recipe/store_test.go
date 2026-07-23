package recipe

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestRecipeStoreCurrentReturnsAReferenceForAFreshRecipe(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}
	rs := &RecipeStore{Repo: repo}

	rec := recipeFixture{id: "pkw:recipe/a/fresh", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	ref, err := rs.Current(rec.Id)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if ref.Type != protocol.ArtifactReferenceTypeRetrievalRecipe {
		t.Fatalf("Type = %q, want retrieval_recipe", ref.Type)
	}
	if ref.Format != protocol.ArtifactReferenceFormatJson {
		t.Fatalf("Format = %q, want json", ref.Format)
	}
	if ref.Version != 1 {
		t.Fatalf("Version = %d, want 1 (default for a record with no RecipeVersion set)", ref.Version)
	}
	if ref.Id != rec.Id {
		t.Fatalf("Id = %q, want %q", ref.Id, rec.Id)
	}
}

func TestRecipeStoreCurrentFollowsTheSupersedeChainToTheHead(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}
	rs := &RecipeStore{Repo: repo}

	v1 := recipeFixture{id: "pkw:recipe/a/v1", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	if _, err := repo.CreateVersion(v1, ""); err != nil {
		t.Fatalf("CreateVersion(v1): %v", err)
	}
	v2 := recipeFixture{id: "pkw:recipe/a/v2", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	two := 2
	v2.RetrievalRecipe.RecipeVersion = &two
	if _, err := repo.CreateVersion(v2, v1.Id); err != nil {
		t.Fatalf("CreateVersion(v2): %v", err)
	}

	// Current(id) must resolve to the head (v2) whether asked about the
	// original id or the new head's own id - a review pinned to the old
	// id before the correction happened must still resolve correctly.
	forOld, err := rs.Current(v1.Id)
	if err != nil {
		t.Fatalf("Current(v1.Id): %v", err)
	}
	if forOld.Id != v2.Id || forOld.Version != 2 {
		t.Fatalf("Current(v1.Id) = %+v, want it to resolve to v2 (version 2)", forOld)
	}

	forNew, err := rs.Current(v2.Id)
	if err != nil {
		t.Fatalf("Current(v2.Id): %v", err)
	}
	if forNew.Id != v2.Id {
		t.Fatalf("Current(v2.Id) = %+v, want v2's own id", forNew)
	}
}

func TestRecipeStoreVersionReadsAnySpecificVersionInTheLineage(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}
	rs := &RecipeStore{Repo: repo}

	v1 := recipeFixture{id: "pkw:recipe/a/v1", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	if _, err := repo.CreateVersion(v1, ""); err != nil {
		t.Fatalf("CreateVersion(v1): %v", err)
	}
	v2 := recipeFixture{id: "pkw:recipe/a/v2", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	two := 2
	v2.RetrievalRecipe.RecipeVersion = &two
	if _, err := repo.CreateVersion(v2, v1.Id); err != nil {
		t.Fatalf("CreateVersion(v2): %v", err)
	}

	// Asking for version 1 from the new head's id must still find the
	// older record via the lineage scan, not just the id passed in.
	content, ref, err := rs.Version(v2.Id, 1)
	if err != nil {
		t.Fatalf("Version(v2.Id, 1): %v", err)
	}
	if ref.Id != v1.Id || ref.Version != 1 {
		t.Fatalf("ref = %+v, want v1's own id and version 1", ref)
	}
	var decoded protocol.KnowledgeRecord
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatalf("content is not valid JSON: %v", err)
	}
	if decoded.Id != v1.Id {
		t.Fatalf("decoded content Id = %q, want %q", decoded.Id, v1.Id)
	}
}

func TestRecipeStoreVersionNotFoundForAnUnknownVersionNumber(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}
	rs := &RecipeStore{Repo: repo}

	rec := recipeFixture{id: "pkw:recipe/a/only", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, _, err := rs.Version(rec.Id, 99); !errors.Is(err, artifact.ErrVersionNotFound) {
		t.Fatalf("err = %v, want ErrVersionNotFound", err)
	}
}

func TestRecipeStoreCurrentNotFoundForAnUnknownID(t *testing.T) {
	store := newTestStore(t)
	rs := &RecipeStore{Repo: &Repository{Store: store}}

	if _, err := rs.Current("no-such-recipe"); !errors.Is(err, ErrRecipeNotFound) {
		t.Fatalf("err = %v, want ErrRecipeNotFound", err)
	}
}

func TestRecipeStoreCreateVersionSupersedesAndReturnsTheNewIdentity(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}
	rs := &RecipeStore{Repo: repo}

	original := recipeFixture{id: "pkw:recipe/a/original", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	if err := store.Put(original); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// The proposed content is a complete recipe record, but CreateVersion
	// must never trust its id field - a reviewing agent has no reason to
	// know this package's own lineage id scheme, and blindly reusing the
	// current head's id (as a naive "just take proposed.Id" would do if
	// the agent echoed the base record back unchanged) would create a
	// self-referential supersede cycle.
	proposed := original
	proposed.RetrievalRecipe.Intent = "corrected.intent"
	content, err := json.Marshal(proposed)
	if err != nil {
		t.Fatalf("marshal proposed: %v", err)
	}

	newRef, err := rs.CreateVersion(original.Id, "punakawan", content, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	if newRef.Id == original.Id {
		t.Fatalf("newRef.Id = %q, want a freshly minted id distinct from the original (CreateVersion mints a new lineage member, it does not rewrite in place)", newRef.Id)
	}
	if newRef.Version != 2 {
		t.Fatalf("newRef.Version = %d, want 2", newRef.Version)
	}

	oldRec, err := store.Get(original.Id)
	if err != nil {
		t.Fatalf("Get(original): %v", err)
	}
	if oldRec.SupersededBy == nil || *oldRec.SupersededBy != newRef.Id {
		t.Fatalf("original.SupersededBy = %v, want it to point at the new version %q", oldRec.SupersededBy, newRef.Id)
	}

	current, err := rs.Current(original.Id)
	if err != nil {
		t.Fatalf("Current(original.Id): %v", err)
	}
	if current.Id != newRef.Id || current.Version != 2 {
		t.Fatalf("Current = %+v, want the corrected version %q", current, newRef.Id)
	}
}

func TestRecipeStoreCreateVersionRejectsContentWithNoRetrievalRecipeBody(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}
	rs := &RecipeStore{Repo: repo}

	original := recipeFixture{id: "pkw:recipe/a/original", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()
	if err := store.Put(original); err != nil {
		t.Fatalf("Put: %v", err)
	}

	malformed, _ := json.Marshal(protocol.KnowledgeRecord{Id: "pkw:recipe/a/malformed"})
	if _, err := rs.CreateVersion(original.Id, "punakawan", malformed, time.Now().UTC()); err == nil {
		t.Fatal("CreateVersion succeeded on content with no retrieval_recipe body, want an error")
	}
}

func TestMarshalCanonicalIsStableLineOrientedJSON(t *testing.T) {
	rec := recipeFixture{id: "pkw:recipe/a/canonical", capability: "jira.issue.search", state: protocol.KnowledgeRecordValidityStateVerified}.build()

	first, err := MarshalCanonical(rec)
	if err != nil {
		t.Fatalf("MarshalCanonical: %v", err)
	}
	second, err := MarshalCanonical(rec)
	if err != nil {
		t.Fatalf("MarshalCanonical (again): %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("MarshalCanonical is not deterministic for the same record")
	}
	if !json.Valid(first) {
		t.Fatal("MarshalCanonical output is not valid JSON")
	}
}
