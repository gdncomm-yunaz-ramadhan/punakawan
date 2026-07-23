package artifact

import (
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

const sampleRecipeJSON = `{
  "id": "pkw:recipe/affiliate-api/jira-next-sprint",
  "type": "retrieval-recipe",
  "retrieval_recipe": {
    "capability": "jira.issue.search",
    "intent": "project.next-sprint.issues",
    "selector": {
      "all": [
        {"field": "project", "operator": "equals", "value": {"literal": "AFF"}}
      ]
    }
  }
}`

func recipeFieldPathAnchor(fieldPath, baseRevisionHash string) protocol.ArtifactCommentAnchor {
	return protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindRecipeFieldPath,
		BaseRevisionHash: baseRevisionHash,
		FieldPath:        &fieldPath,
	}
}

func TestResolveAnchorRecipeFieldPathExactMatch(t *testing.T) {
	anchor := recipeFieldPathAnchor("retrieval_recipe.selector.all.0.value.literal", Hash([]byte(sampleRecipeJSON)))

	block, method := ResolveAnchor(sampleRecipeJSON, anchor)
	if method != AnchorResolvedByFieldPath {
		t.Fatalf("method = %q, want field_path", method)
	}
	if block.Content != `"AFF"` {
		t.Fatalf("Content = %q, want the quoted literal value", block.Content)
	}
	if block.ID != "retrieval_recipe.selector.all.0.value.literal" {
		t.Fatalf("ID = %q, want the field_path itself", block.ID)
	}
}

func TestResolveAnchorRecipeFieldPathObjectValue(t *testing.T) {
	anchor := recipeFieldPathAnchor("retrieval_recipe.selector.all.0", Hash([]byte(sampleRecipeJSON)))

	block, method := ResolveAnchor(sampleRecipeJSON, anchor)
	if method != AnchorResolvedByFieldPath {
		t.Fatalf("method = %q, want field_path", method)
	}
	if block.Content == "" {
		t.Fatal("Content is empty, want the selector clause's JSON")
	}
}

func TestResolveAnchorRecipeFieldPathConflictsWhenPathDoesNotExist(t *testing.T) {
	anchor := recipeFieldPathAnchor("retrieval_recipe.selector.all.99.value", Hash([]byte(sampleRecipeJSON)))

	_, method := ResolveAnchor(sampleRecipeJSON, anchor)
	if method != AnchorConflicted {
		t.Fatalf("method = %q, want conflicted for a nonexistent path", method)
	}
}

func TestResolveAnchorRecipeFieldPathConflictsWithNoFieldPath(t *testing.T) {
	anchor := protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindRecipeFieldPath,
		BaseRevisionHash: Hash([]byte(sampleRecipeJSON)),
	}

	_, method := ResolveAnchor(sampleRecipeJSON, anchor)
	if method != AnchorConflicted {
		t.Fatalf("method = %q, want conflicted when no field_path is given", method)
	}
}

func TestResolveAnchorRecipeFieldPathConflictsOnInvalidJSON(t *testing.T) {
	anchor := recipeFieldPathAnchor("retrieval_recipe.selector.all.0", Hash([]byte("not json")))

	_, method := ResolveAnchor("not json", anchor)
	if method != AnchorConflicted {
		t.Fatalf("method = %q, want conflicted for invalid JSON content", method)
	}
}

func TestResolveAnchorDispatchesByKind(t *testing.T) {
	// A markdown_block anchor against JSON content (or vice versa) must
	// not accidentally "succeed" - ResolveAnchor's dispatch on anchor.Kind
	// is what keeps the two resolution paths from bleeding into each
	// other.
	markdownAnchor := protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindMarkdownBlock,
		BaseRevisionHash: Hash([]byte(sampleRecipeJSON)),
		BlockId:          strp("retrieval_recipe.selector.all.0"),
	}
	_, method := ResolveAnchor(sampleRecipeJSON, markdownAnchor)
	if method != AnchorConflicted {
		t.Fatalf("method = %q, want conflicted (markdown_block resolution against JSON content finds no pk:block markers)", method)
	}
}
