/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One append-only, idempotent state-transition event for a DeliveryOrchestration or one of its scoped sub-entities (lane, requirement source, parent task, dependency edge). Orchestration and sub-entity state is derived by replaying these in sequence order; the same idempotency_key is applied at most once.
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
    | "orchestration.completed_with_gaps"
    | "orchestration.details_updated"
    | "project.attached"
    | "project.detached"
    | "input.registered"
    | "input.resolved"
    | "lane.created"
    | "lane.status_changed"
    | "lane.blocked"
    | "lane.unblocked"
    | "lane.worktree_created"
    | "lane.worktree_removed"
    | "lane.semar_submitted"
    | "lane.gareng_submitted"
    | "lane.petruk_submitted"
    | "lane.bagong_submitted"
    | "lane.verification_dimension_recorded"
    | "lane.ci_check_reported"
    | "lane.review_conclusion_recorded"
    | "lane.commit_recorded"
    | "lane.pr_published"
    | "lane.repair_cycle_started"
    | "lane.escalated"
    | "lease.granted"
    | "lease.heartbeat"
    | "lease.completed"
    | "lease.rejected"
    | "lease.timed_out"
    | "lease.cancelled"
    | "worklog.recorded"
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
