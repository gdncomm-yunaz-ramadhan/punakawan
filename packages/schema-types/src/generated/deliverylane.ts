/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One project's independent delivery lane within a DeliveryOrchestration (punokawan-14yn.1), and the schedulable unit punokawan-14yn.3's worker scheduler leases and advances through waiting/blocked/runnable/leased/running/review/failed/accepted. A lane always carries its own project_id so cross-project reads and writes fail closed. Worktree lifecycle and role execution are later tasks (punokawan-14yn.5/6). See affiliate-platform-delivery-feedback-2026-08-07.md.
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
  /**
   * punokawan-14yn.3's scheduling state machine. waiting: predecessors unresolved. blocked: an unresolved hard blocker was reported with evidence. runnable: on the frontier, no worker leased yet. leased: a worker holds an unexpired lease but has not reported starting. running: the leased worker is actively executing. review: work reported complete, awaiting Bagong/human review (punokawan-14yn.8). failed: rejected, expired past retry budget, or reported failed. accepted: terminal success.
   */
  status: "waiting" | "blocked" | "runnable" | "leased" | "running" | "review" | "failed" | "accepted";
  /**
   * Worker holding the current lease; empty when not leased/running.
   */
  lease_worker_id?: string;
  /**
   * Opaque token the leaseholder must present to heartbeat/complete/reject - proves it still holds this lease, not a stale or resumed one.
   */
  lease_token?: string;
  /**
   * Lease/heartbeat deadline; past this with no renewal, the lease is expired and the lane returns to runnable.
   */
  lease_expires_at?: string;
  /**
   * Number of leases granted so far for this lane; incremented on retry.
   */
  attempt?: number;
  /**
   * Exact blocker ids (unresolved predecessor task ids, or a reported discovered blocker) while status is waiting or blocked.
   */
  blocked_by?: string[];
  created_at: string;
  updated_at: string;
  /**
   * Optimistic-concurrency counter; incremented on every applied event.
   */
  revision: number;
}
