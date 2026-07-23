package api

import (
	"errors"
	"fmt"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/recipe"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// ArtifactStores bundles every artifact.Store this panel build has wired
// up, keyed by the artifact type it serves. It replaces handlers'
// previous hardcoded *artifact.PlanStore parameter: every artifact-review
// handler now takes an ArtifactStores and looks up the right concrete
// store via resolveArtifactType, per punokawan-q9r.6's instruction to
// make the existing review/proposal protocol type-generic rather than
// build a second, retrieval_recipe-specific copy of it.
//
// Recipes is a factory, not a concrete store: opening the Dolt-backed
// knowledge store behind it starts an external Dolt server process
// (App.OpenKnowledge's own doc comment), which every existing plan-only
// route (the overwhelming majority of panel traffic, including nearly
// every pre-existing test) has no reason to pay for. Recipes is called
// lazily, only when a retrieval_recipe-typed request actually arrives,
// and is expected to memoize its own result (App.OpenKnowledge already
// does) so repeated calls are cheap. A nil Recipes, or one that errors,
// degrades only retrieval_recipe-typed requests to a 400 - "Panel
// failures do not affect recipe execution in core" (punokawan-q9r.6's
// own acceptance criterion) - never the rest of the panel.
type ArtifactStores struct {
	Plans   *artifact.PlanStore
	Recipes func() (*recipe.RecipeStore, error)
}

// resolveArtifactType maps a {type} path segment to the matching store,
// or a descriptive error if the type is unknown or its store was never
// wired up.
func resolveArtifactType(stores ArtifactStores, artifactType string) (artifact.Store, protocol.ArtifactReviewArtifactType, error) {
	switch artifactType {
	case string(protocol.ArtifactReviewArtifactTypePlan):
		if stores.Plans == nil {
			return nil, "", fmt.Errorf("api: no plan store configured")
		}
		return stores.Plans, protocol.ArtifactReviewArtifactTypePlan, nil
	case string(protocol.ArtifactReviewArtifactTypeRetrievalRecipe):
		if stores.Recipes == nil {
			return nil, "", fmt.Errorf("api: retrieval_recipe review is not available in this workspace (no knowledge store configured)")
		}
		recipeStore, err := stores.Recipes()
		if err != nil {
			return nil, "", fmt.Errorf("api: retrieval_recipe review is not available in this workspace: %w", err)
		}
		return recipeStore, protocol.ArtifactReviewArtifactTypeRetrievalRecipe, nil
	default:
		return nil, "", fmt.Errorf("api: unsupported artifact type %q (want %q or %q)", artifactType, protocol.ArtifactReviewArtifactTypePlan, protocol.ArtifactReviewArtifactTypeRetrievalRecipe)
	}
}

// storeFor resolves review's own artifact.type to the matching store,
// for handlers operating on an existing review/comment/proposal rather
// than a fresh {type}/{id} path pair.
func storeFor(stores ArtifactStores, artifactType protocol.ArtifactReviewArtifactType) (artifact.Store, error) {
	store, _, err := resolveArtifactType(stores, string(artifactType))
	return store, err
}

// isArtifactNotFound reports whether err is either store's own
// not-found sentinel - the handlers that call this only need to know
// "404 or not," not which concrete store produced it.
func isArtifactNotFound(err error) bool {
	return errors.Is(err, artifact.ErrPlanNotFound) || errors.Is(err, recipe.ErrRecipeNotFound)
}
