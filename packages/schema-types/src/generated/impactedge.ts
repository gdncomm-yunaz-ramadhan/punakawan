/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One evidence-backed directed edge in a project's Cross-Repository Impact Graph, per punakawan-role-config-distinguished-improvements-plan.md Part III §25/§26. Edges connect two ImpactNode ids and carry a confidence and the evidence that discovered them, so an impact query can distinguish observed facts from inferences. Persisted append-only to .punakawan/impact/edges.jsonl.
 */
export interface ImpactEdge {
  /**
   * Source node id.
   */
  from: string;
  /**
   * Target node id.
   */
  to: string;
  type:
    | "contains"
    | "defines"
    | "calls"
    | "implements"
    | "consumes"
    | "tests"
    | "configures"
    | "deploys"
    | "depends_on"
    | "documented_by"
    | "owned_by"
    | "tracked_by"
    | "contradicts"
    | "derived_from";
  /**
   * observed: directly seen in a source. inferred: derived heuristically. verified: independently confirmed. disputed: a contradiction exists about this edge.
   */
  confidence: "observed" | "inferred" | "verified" | "disputed";
  evidence?: {
    /**
     * e.g. source_location, openapi_reference, test_reference.
     */
    type: string;
    /**
     * e.g. src/lib/api/merchant.ts:44.
     */
    ref?: string;
  }[];
  discovered_by?: {
    role?: string;
    /**
     * e.g. openapi-client-reference.
     */
    method?: string;
  };
}
