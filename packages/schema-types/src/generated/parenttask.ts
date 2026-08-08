/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One node in an orchestration's task dependency graph (punokawan-14yn.2): a group of one or more RequirementSources routed, or awaiting routing, to a single project. A DeliveryLane (punokawan-14yn.1) is created once a ParentTask is routed and its DAG position allows execution to begin - lane/worker execution status is not duplicated here. See affiliate-platform-delivery-feedback-2026-08-07.md.
 */
export interface ParentTask {
  /**
   * Filesystem-safe ULID (Crockford base32, 26 chars).
   */
  id: string;
  orchestration_id: string;
  /**
   * Set once routing resolves this task to a project; absent while unrouted.
   */
  project_id?: string;
  title: string;
  /**
   * RequirementSource ids grouped into this task.
   */
  source_ids: string[];
  status: "unrouted" | "routed" | "cancelled";
  created_at: string;
  updated_at: string;
  /**
   * Optimistic-concurrency counter; incremented on every applied event.
   */
  revision: number;
}
