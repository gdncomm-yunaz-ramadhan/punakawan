/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One project's independent delivery lane within a DeliveryOrchestration, and the schedulable unit the worker scheduler leases and advances through waiting/blocked/runnable/leased/running/review/failed/accepted. A lane always carries its own project_id so cross-project reads and writes fail closed.
 */
export interface DeliveryLane {
  /**
   * Filesystem-safe ULID (Crockford base32, 26 chars).
   */
  id: string;
  orchestration_id: string;
  project_id: string;
  /**
   * Id of the parent task this lane delivers, assigned once the dependency graph resolves it. Empty until then.
   */
  parent_task_id?: string;
  /**
   * The scheduling state machine. waiting: predecessors unresolved. blocked: an unresolved hard blocker was reported with evidence. runnable: no unresolved predecessor, no worker leased yet. leased: a worker holds an unexpired lease but has not reported starting. running: the leased worker is actively executing. review: work reported complete, awaiting review. failed: rejected, expired past retry budget, or reported failed. accepted: terminal success.
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
  /**
   * The lane's own git branch name, set once its worktree is first created; persists even after the worktree directory is later removed, so a later resume checks out the same branch instead of creating a new one.
   */
  branch?: string;
  /**
   * Absolute path to the lane's current linked worktree directory; present only while the worktree exists on disk, cleared (but branch/base_sha/base_remote kept) when removed.
   */
  worktree_path?: string;
  /**
   * The exact commit the branch was forked from.
   */
  base_sha?: string;
  /**
   * The remote name (e.g. "origin") the base branch was fetched from.
   */
  base_remote?: string;
  /**
   * Id of the knowledge record holding Semar's synthesis for this lane's current attempt. Cleared, along with every later stage below, whenever it is resubmitted.
   */
  semar_record_id?: string;
  /**
   * Id of the knowledge record holding Gareng's feasibility review for this lane's current attempt. Cleared, along with petruk_record_id and bagong_record_id, whenever it is resubmitted.
   */
  gareng_record_id?: string;
  /**
   * Id of the knowledge record holding Petruk's plan for this lane's current attempt. Cleared, along with bagong_record_id, whenever it is resubmitted.
   */
  petruk_record_id?: string;
  /**
   * Id of the knowledge record holding Bagong's independent review for this lane's current attempt. A held lease cannot be completed until this is set.
   */
  bagong_record_id?: string;
  /**
   * Which provider the lane's published pull request lives on. Absent until a pull request has actually been published for this lane.
   */
  pr_provider?: "github" | "gitlab" | "generic";
  /**
   * The repository the lane's published pull request was opened against, in the provider's own repository-identifier form (e.g. "owner/repo" for GitHub).
   */
  pr_repo_slug?: string;
  /**
   * The provider's own number for the lane's published pull request. Once set, a lane never opens a second pull request for the same attempt.
   */
  pr_number?: number;
  /**
   * A link to the lane's published pull request.
   */
  pr_url?: string;
  /**
   * Number of repair cycles started for this lane so far. Absent is equivalent to zero.
   */
  repair_cycle_count?: number;
  /**
   * When this lane was last escalated for a human to look at, e.g. after exhausting its repair-cycle budget. Absent while the lane has never been escalated. Escalation never changes the lane's own scheduling status.
   */
  escalated_at?: string;
}
