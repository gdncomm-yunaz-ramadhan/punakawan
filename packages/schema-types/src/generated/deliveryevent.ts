/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One append-only, idempotent state-transition event for a DeliveryOrchestration or one of its scoped sub-entities (lane, requirement source, parent task, dependency edge - punokawan-14yn.1/.2). Orchestration and sub-entity state is derived by replaying these in sequence order; the same idempotency_key is applied at most once. See affiliate-platform-delivery-feedback-2026-08-07.md.
 */
export interface DeliveryEvent {
  /**
   * Filesystem-safe ULID (Crockford base32, 26 chars).
   */
  id: string;
  orchestration_id: string;
  /**
   * Set for events scoped to one lane, requirement source, parent task, or dependency edge; absent for orchestration-scoped events.
   */
  entity_id?: string;
  idempotency_key: string;
  type:
    | "orchestration.created"
    | "orchestration.cancelled"
    | "orchestration.completed"
    | "input.registered"
    | "input.resolved"
    | "lane.created"
    | "lane.status_changed"
    | "requirement.captured"
    | "task.created"
    | "task.routed"
    | "edge.added"
    | "edge.removed";
  payload: {
    [k: string]: unknown;
  };
  /**
   * Monotonic per orchestration_id; determines deterministic replay order.
   */
  sequence: number;
  occurred_at: string;
}
