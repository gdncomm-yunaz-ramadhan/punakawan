/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One project's independent delivery lane within a DeliveryOrchestration (punokawan-14yn.1). A lane always carries its own project_id so cross-project reads and writes fail closed. Worktree lifecycle, worker scheduling, and role execution are later tasks (punokawan-14yn.3/5); this task only defines and persists lane identity and status. See affiliate-platform-delivery-feedback-2026-08-07.md.
 */
export interface DeliveryLane {
  /**
   * Filesystem-safe ULID (Crockford base32, 26 chars).
   */
  id: string;
  orchestration_id: string;
  project_id: string;
  /**
   * Id of the parent task this lane delivers, assigned once punokawan-14yn.2's dependency graph resolves it. Empty until then.
   */
  parent_task_id?: string;
  status: "pending" | "active" | "cancelled" | "completed" | "failed";
  created_at: string;
  updated_at: string;
  /**
   * Optimistic-concurrency counter; incremented on every applied event.
   */
  revision: number;
}
