// recipe_field_path anchor helpers, the retrieval-recipe equivalent of
// markdown.ts's heading-path/block-id helpers for plans. Unlike a plan's
// Markdown, a recipe's canonical content (internal/recipe.RecipeStore.
// MarshalCanonical) is indented JSON of the *whole* protocol.KnowledgeRecord,
// so a field_path is a gjson-syntax dotted path into that document (e.g.
// "retrieval_recipe.selector.all.0.value.literal"), resolved server-side by
// internal/artifact/recipefieldpath.go's single exact-match lookup - there is
// no fuzzy fallback chain like markdown_block's, so the path this client
// sends must exactly match a path that exists in the reviewed revision's
// serialized content.

export interface RecipeFieldAnchorInput {
  baseRevisionHash: string;
  fieldPath: string;
}

export interface RecipeFieldCommentAnchor {
  kind: "recipe_field_path";
  base_revision_hash: string;
  field_path: string;
}

// buildFieldPathAnchor constructs the exact JSON shape the server's
// POST /reviews/{id}/comments endpoint expects for a retrieval_recipe
// review (protocol.ArtifactCommentAnchor with kind: "recipe_field_path").
export function buildFieldPathAnchor(input: RecipeFieldAnchorInput): RecipeFieldCommentAnchor {
  return {
    kind: "recipe_field_path",
    base_revision_hash: input.baseRevisionHash,
    field_path: input.fieldPath,
  };
}

// joinFieldPath renders a gjson-syntax path from its segments, matching
// how internal/artifact/recipefieldpath.go and the seeded e2e test build
// paths like "retrieval_recipe.selector.all.0.value.literal" - object keys
// and array indices are both "."-separated, with no bracket syntax.
export function joinFieldPath(...segments: (string | number)[]): string {
  return segments.map(String).join(".");
}

type JSONValue = string | number | boolean | null | JSONValue[] | { [key: string]: JSONValue };

// allFieldPaths walks parsed JSON content depth-first (matching
// RecipeDocument's own render order) and returns every node's field_path
// in that order - used as CommentRail's documentHeadingOrder equivalent,
// so recipe comments group/order by their position in the document
// rather than creation order, the same as a plan's heading order.
export function allFieldPaths(content: string): string[] {
  let parsed: JSONValue;
  try {
    parsed = JSON.parse(content) as JSONValue;
  } catch {
    return [];
  }
  const paths: string[] = [];
  function walk(value: JSONValue, path: string) {
    if (path) paths.push(path);
    if (value === null || typeof value !== "object") return;
    if (Array.isArray(value)) {
      value.forEach((item, i) => walk(item, joinFieldPath(path, i)));
    } else {
      for (const [key, item] of Object.entries(value)) {
        walk(item, path ? joinFieldPath(path, key) : key);
      }
    }
  }
  walk(parsed, "");
  return paths;
}
