package api

import (
	"errors"
	"testing"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/recipe"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestResolveArtifactTypeDispatchesPlan(t *testing.T) {
	plans := &artifact.PlanStore{WorkspaceRoot: t.TempDir()}
	stores := ArtifactStores{Plans: plans}

	store, artifactType, err := resolveArtifactType(stores, "plan")
	if err != nil {
		t.Fatalf("resolveArtifactType: %v", err)
	}
	if store != artifact.Store(plans) {
		t.Fatal("resolveArtifactType did not return the configured plan store")
	}
	if artifactType != protocol.ArtifactReviewArtifactTypePlan {
		t.Fatalf("artifactType = %q, want plan", artifactType)
	}
}

func TestResolveArtifactTypeRejectsUnknownType(t *testing.T) {
	stores := ArtifactStores{Plans: &artifact.PlanStore{WorkspaceRoot: t.TempDir()}}
	if _, _, err := resolveArtifactType(stores, "spreadsheet"); err == nil {
		t.Fatal("resolveArtifactType succeeded for an unknown type, want an error")
	}
}

func TestResolveArtifactTypeDegradesGracefullyWithNoRecipesFactory(t *testing.T) {
	// A workspace that never wired up a knowledge store (Recipes is nil)
	// must fail only the retrieval_recipe-typed lookup, not panic or
	// affect plan resolution - "panel failures do not affect recipe
	// execution in core" (punokawan-q9r.6's own acceptance criterion).
	stores := ArtifactStores{Plans: &artifact.PlanStore{WorkspaceRoot: t.TempDir()}}
	if _, _, err := resolveArtifactType(stores, "retrieval_recipe"); err == nil {
		t.Fatal("resolveArtifactType succeeded for retrieval_recipe with no Recipes factory configured, want an error")
	}
}

func TestResolveArtifactTypeSurfacesRecipesFactoryError(t *testing.T) {
	boom := errors.New("dolt failed to start")
	stores := ArtifactStores{
		Recipes: func() (*recipe.RecipeStore, error) { return nil, boom },
	}
	_, _, err := resolveArtifactType(stores, "retrieval_recipe")
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap the factory's own error", err)
	}
}

func TestResolveArtifactTypeCallsRecipesFactoryLazily(t *testing.T) {
	calls := 0
	stores := ArtifactStores{
		Recipes: func() (*recipe.RecipeStore, error) {
			calls++
			return &recipe.RecipeStore{}, nil
		},
	}
	if calls != 0 {
		t.Fatal("Recipes factory was called before resolveArtifactType ever ran")
	}
	if _, _, err := resolveArtifactType(stores, "retrieval_recipe"); err != nil {
		t.Fatalf("resolveArtifactType: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want exactly 1", calls)
	}
}
