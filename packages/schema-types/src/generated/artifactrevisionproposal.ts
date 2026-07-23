/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One revision attempt's result, not yet a new canonical version. See punakawan-artifact-review-plan-mutation-plan-v2.md §5.4/§9. base.revision_hash is compared against the artifact's current canonical hash at acceptance time (§12); a mismatch means the review is conflicted, never a silent overwrite.
 */
export interface ArtifactRevisionProposal {
  metadata: {
    id: string;
    review_id: string;
    revision_request_id: string;
    attempt: number;
    status: "pending" | "ready" | "failed" | "superseded";
  };
  base: {
    artifact_id: string;
    version: number;
    revision_hash: string;
  };
  proposed: {
    version: number;
    content_hash: string;
    content_location: string;
    change_summary?: string;
  };
  results?: {
    addressed_comments?: number;
    partially_addressed_comments?: number;
    unresolved_comments?: number;
    validation_status?: "pending" | "passed" | "failed";
    comment_resolutions?: {
      comment_id: string;
      status: "addressed" | "partially_addressed" | "rejected" | "not_applicable";
      explanation?: string;
      changed_block_ids?: string[];
    }[];
  };
}
