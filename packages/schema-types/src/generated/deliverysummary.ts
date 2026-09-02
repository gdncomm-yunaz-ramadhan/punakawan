/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One delivery's compact panel-list row: enough to render, search, and sort every delivery without a per-card follow-up fetch. Carries no scheduler-internal concepts (lanes, blocked counts, pending questions, a lane-derived next action).
 */
export interface DeliverySummary {
  id: string;
  title: string;
  status: "pending" | "active" | "completed" | "cancelled";
  /**
   * The delivery's originating Jira issue. Absent for an ad-hoc delivery with no single originating issue.
   */
  source?: {
    kind: "jira" | "adhoc";
    key?: string;
    title?: string;
    /**
     * The last locally observed Jira status - the target status of the most recent transition this delivery itself requested, never a live Jira read.
     */
    status?: string;
  };
  projects: {
    id: string;
    slug: string;
  }[];
  /**
   * The delivery's own cross-project high-level plan link.
   */
  plan?: {
    id: string;
    revision: number;
    objective: string;
    status?: string;
  };
  workflow?: {
    id: string;
    name: string;
  };
  progress?: {
    percent?: number;
    summary: string;
    reported_at: string;
  };
  session?: {
    participant?: string;
    provider?: string;
    model?: string;
    status: string;
    started_at: string;
    stopped_at?: string;
  };
  usage: {
    input_tokens: number;
    output_tokens: number;
    cache_tokens: number;
    tool_calls: number;
    elapsed_ms: number;
    /**
     * Currency to amount. More than one entry means the delivery's contributing sessions priced in more than one currency; empty when no contributing snapshot ever named a cost at all.
     */
    estimated_costs: {
      [k: string]: number;
    };
    /**
     * False whenever any contributing usage was priced against an unknown model rate - the totals are then a partial, never a fabricated, sum.
     */
    pricing_complete: boolean;
    /**
     * Model ids that could not be priced, sorted and deduplicated. pricing_complete says the sum is partial; this says which model to add to the catalog to make it whole.
     */
    unpriced_models?: string[];
  };
  updated_at: string;
  cancellable: boolean;
  /**
   * delivery_projection_versions' current revision for this delivery - the number a caller compares across list/detail calls, and passes back as since_revision to the watch endpoint.
   */
  projection_revision: number;
}
