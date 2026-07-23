/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * A review session against one artifact version. See punakawan-artifact-review-plan-mutation-plan-v2.md §5.1.
 */
export interface ArtifactReview {
  metadata: {
    id: string;
    workspace_id: string;
    status:
      | "draft"
      | "submitted"
      | "queued"
      | "revising"
      | "awaiting_clarification"
      | "proposal_ready"
      | "revision_requested"
      | "accepted"
      | "rejected"
      | "cancelled"
      | "failed"
      | "conflicted";
    created_by: string;
    created_at: string;
    updated_at?: string;
  };
  artifact: {
    type: "plan" | "retrieval_recipe";
    id: string;
    version: number;
    revision_hash: string;
  };
  review: {
    title: string;
    instruction?: string;
    comment_count?: number;
  };
}
