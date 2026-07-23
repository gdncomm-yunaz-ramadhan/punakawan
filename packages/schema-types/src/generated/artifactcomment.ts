/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One anchored review comment. See punakawan-artifact-review-plan-mutation-plan-v2.md §5.2/§6. anchor.block_id/base_revision_hash/heading_path/quoted_text together drive the 5-step anchor resolution order (§6): exact block ID, exact content hash, heading path plus quoted text, heading path plus fuzzy quoted text, or conflicted.
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
    kind: "markdown_block";
    block_id?: string;
    heading_path?: string[];
    base_revision_hash: string;
    quoted_text?: string;
  };
  body: string;
}
