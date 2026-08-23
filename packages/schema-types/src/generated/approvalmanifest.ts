/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One immutable approval request/decision for a single project within a DeliveryOrchestration: the parent tasks it covers, the planned base ref/branches/writes, and the preflight checks it was computed against. Approving one manifest never authorizes a newly discovered project or a write category beyond what this manifest declares - a changed scope requires a new manifest, not an edit to this one.
 */
export interface ApprovalManifest {
  /**
   * Filesystem-safe ULID (Crockford base32, 26 chars).
   */
  id: string;
  orchestration_id: string;
  project_id: string;
  /**
   * Every ParentTask this manifest covers; all must already be routed to project_id.
   */
  parent_task_ids: string[];
  planned_base_ref: string;
  planned_branches?: string[];
  expects_jira_writes?: boolean;
  expects_commits?: boolean;
  expects_pushes?: boolean;
  expects_prs?: boolean;
  /**
   * Snapshot of the PreflightCheck results (preflightcheck.schema.json) this manifest was computed against, duplicated inline rather than by $ref - this repo does not use cross-file $ref (see knowledge.schema.json's selector comment) and a manifest's checks are a point-in-time copy, not a live reference anyway.
   */
  checks: {
    name: string;
    status: "pass" | "fail" | "skipped";
    classification: "required" | "optional" | "delegated-to-ci";
    detail?: string;
  }[];
  status: "pending" | "approved" | "rejected";
  /**
   * Never one of the agent role identifiers (semar/gareng/petruk/bagong) - an agent may not approve its own manifest.
   */
  approved_by?: string;
  decided_at?: string;
  created_at: string;
  /**
   * Optimistic-concurrency counter; incremented on every applied event.
   */
  revision: number;
  /**
   * Total verified-work hours (internal/worklogalloc), computed from this project's completed test-run command durations, that the proposed worklog below is derived from. Zero when no test-run evidence has accumulated yet for this manifest's parent tasks - proposed worklogs must be visible before project approval.
   */
  proposed_worklog_total_hours?: number;
  /**
   * The dev/test/review split of proposed_worklog_total_hours that could be matched to one of this project's configured Jira subtasks by name (internal/worklogalloc.Allocate) - a proposal for update_jira_task_progress to post post-approval, not something this manifest itself writes to Jira. Coarse by construction: internal/testrun records only that a command ran and how long it took, with no dev/test/review distinction, so the total is split evenly across whichever buckets have a matching subtask, never a fabricated precise breakdown.
   */
  proposed_worklog?: {
    bucket: "dev" | "test" | "review";
    /**
     * The configured Jira subtask (from list_jira_subtasks) this bucket's hours would be logged against.
     */
    subtask_key: string;
    subtask_name?: string;
    hours: number;
  }[];
  /**
   * Hours from proposed_worklog_total_hours that could not be matched to any configured subtask (no subtask name contained a recognized dev/test/review keyword) - left unmapped rather than guessed onto an arbitrary subtask.
   */
  proposed_worklog_unmapped_hours?: number;
}
