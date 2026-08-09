/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One continuous multi-project delivery run (punokawan-14yn.1). Owns zero or more DeliveryLanes and a list of not-yet-routed requirement inputs. State is derived by replaying its DeliveryEvent log, never written directly. See affiliate-platform-delivery-feedback-2026-08-07.md.
 */
export interface DeliveryOrchestration {
  /**
   * Filesystem-safe ULID (Crockford base32, 26 chars).
   */
  id: string;
  status: "pending" | "active" | "cancelled" | "completed";
  /**
   * Requirement sources (Jira/Confluence/GitHub/URL/free-text) not yet routed to a project. Routing and normalization is punokawan-14yn.2's concern; this task only persists the raw reference.
   */
  unresolved_inputs: {
    reference: string;
    note?: string;
  }[];
  created_at: string;
  updated_at: string;
  /**
   * Optimistic-concurrency counter; incremented on every applied event.
   */
  revision: number;
  /**
   * Id of a saved workflow definition this run was configured from, if any. When set, its per-role restrictions decide which of the four role stages a lane must complete before its lease can be marked done, in place of the default of requiring all four. Absent when no such definition was attached.
   */
  workflow_definition_id?: string;
}
