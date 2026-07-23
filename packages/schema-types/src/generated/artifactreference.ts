/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * Points at one immutable version of a reviewable artifact. See punakawan-artifact-review-plan-mutation-plan-v2.md §4. Every artifact type this record can point at must provide a version reader, stable anchor resolver, proposal renderer, diff generator, validator, and acceptance handler (§4) - this schema only carries the pointer, not that behavior.
 */
export interface ArtifactReference {
  /**
   * Artifact type. Only "plan" is implemented by this plan; "retrieval_recipe" is reserved for punakawan-procedural-knowledge-retrieval-recipe-plan-final.md's own review/mutation reuse once its compiler and validation lifecycle exist (§4), not enabled here.
   */
  type: "plan" | "retrieval_recipe";
  id: string;
  version: number;
  revision_hash: string;
  workspace_id: string;
  format: "markdown";
  /**
   * Path to this version's immutable content, e.g. .punakawan/plans/<id>/versions/<version>.md (§7). Absent for a retrieval_recipe artifact, whose canonical version lives in durable knowledge instead (§7).
   */
  canonical_location?: string;
}
