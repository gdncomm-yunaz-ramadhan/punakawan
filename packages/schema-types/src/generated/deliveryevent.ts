/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One append-only, idempotent state-transition event for a DeliveryOrchestration or one of its DeliveryLanes (punokawan-14yn.1). Orchestration and lane state is derived by replaying these in sequence order; the same idempotency_key is applied at most once. See affiliate-platform-delivery-feedback-2026-08-07.md.
 */
export interface DeliveryEvent {
  /**
   * Filesystem-safe ULID (Crockford base32, 26 chars).
   */
  id: string;
  orchestration_id: string;
  /**
   * Set for lane-scoped events; absent for orchestration-scoped events.
   */
  lane_id?: string;
  idempotency_key: string;
  type:
    | "orchestration.created"
    | "orchestration.cancelled"
    | "orchestration.completed"
    | "input.registered"
    | "input.resolved"
    | "lane.created"
    | "lane.status_changed";
  payload: {
    [k: string]: unknown;
  };
  /**
   * Monotonic per orchestration_id; determines deterministic replay order.
   */
  sequence: number;
  occurred_at: string;
}
