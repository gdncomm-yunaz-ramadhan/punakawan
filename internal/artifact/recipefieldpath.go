package artifact

import (
	"strings"

	"github.com/tidwall/gjson"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// resolveRecipeFieldPathAnchor implements the recipe_field_path half of
// ResolveAnchor's single resolution entrypoint (§6 of the artifact-review
// plan, extended by punakawan-procedural-knowledge-retrieval-recipe-plan-final.md
// Phase 5 for structured retrieval-recipe content instead of Markdown).
//
// Unlike markdown_block's 5-step fuzzy-fallback chain, a structured
// document either has the exact field_path against the exact reviewed
// revision or it does not - there is no heading-path/fuzzy-text
// equivalent for JSON, so this is deliberately a single exact lookup, not
// a chain. content is the recipe's canonical JSON serialization (see
// internal/recipe.RecipeStore's format, matching `punakawan knowledge
// recipe show`'s existing rendering) for the exact version the comment's
// anchor.base_revision_hash names.
func resolveRecipeFieldPathAnchor(content string, anchor protocol.ArtifactCommentAnchor) (Block, AnchorResolution) {
	if anchor.FieldPath == nil || strings.TrimSpace(*anchor.FieldPath) == "" {
		return Block{}, AnchorConflicted
	}
	if !gjson.Valid(content) {
		return Block{}, AnchorConflicted
	}
	result := gjson.Get(content, *anchor.FieldPath)
	if !result.Exists() {
		return Block{}, AnchorConflicted
	}
	return Block{
		ID:          *anchor.FieldPath,
		Content:     result.Raw,
		ContentHash: Hash([]byte(result.Raw)),
	}, AnchorResolvedByFieldPath
}
