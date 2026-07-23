/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One anchored review comment. See punakawan-artifact-review-plan-mutation-plan-v2.md §5.2/§6. For a markdown_block anchor, block_id/base_revision_hash/heading_path/quoted_text together drive the 5-step anchor resolution order (§6): exact block ID, exact content hash, heading path plus quoted text, heading path plus fuzzy quoted text, or conflicted. For a recipe_field_path anchor (punakawan-procedural-knowledge-retrieval-recipe-plan-final.md Phase 5), field_path plus base_revision_hash locate an exact field/condition in a retrieval_recipe's structured content instead: structured data either has the exact path against the reviewed revision or it does not, so no fuzzy fallback chain applies.
 */
export interface ArtifactComment {
  id: string;
  review_id: string;
  author: string;
  status:
    | "open"
    | "addressed"
    | "partially_addressed"
    | "rejected_by_agent"
    | "needs_clarification"
    | "obsolete"
    | "resolved_by_user";
  anchor: {
    kind: "markdown_block" | "recipe_field_path";
    block_id?: string;
    heading_path?: string[];
    base_revision_hash: string;
    quoted_text?: string;
    /**
     * recipe_field_path anchors only: a dotted path (gjson syntax: object keys and array indices both separated by ".", e.g. "selector.all.0.value" or "inputs.1.required") into the recipe's canonical JSON serialization. Resolved by exact match against the reviewed revision's serialized content - see internal/artifact's recipe field-path resolver.
     */
    field_path?: string;
  };
  body: string;
}
