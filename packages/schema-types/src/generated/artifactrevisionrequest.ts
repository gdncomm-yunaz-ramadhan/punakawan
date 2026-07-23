/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * An immutable snapshot created when a review is submitted for revision. See punakawan-artifact-review-plan-mutation-plan-v2.md §5.3/§8. comments.snapshot_hash freezes the comment set at submission time - later comment edits do not retroactively change what an in-flight revision run is working from.
 */
export interface ArtifactRevisionRequest {
  metadata: {
    id: string;
    review_id: string;
    submitted_at: string;
    submitted_by: string;
  };
  base_artifact: {
    type: "plan" | "retrieval_recipe";
    id: string;
    version: number;
    revision_hash: string;
  };
  workflow: {
    type: "revise_plan_from_review" | "revise_retrieval_recipe_from_review";
    auto_start?: boolean;
    require_final_acceptance?: boolean;
    max_revision_attempts?: number;
  };
  comments: {
    snapshot_hash: string;
    count: number;
  };
}
