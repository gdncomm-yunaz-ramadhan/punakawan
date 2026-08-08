/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One immutable approval request/decision for a single project within a DeliveryOrchestration (punokawan-14yn.4): the parent tasks it covers, the planned base ref/branches/writes, and the preflight checks it was computed against. Approving one manifest never authorizes a newly discovered project or a write category beyond what this manifest declares - a changed scope requires a new manifest, not an edit to this one. See affiliate-platform-delivery-feedback-2026-08-07.md.
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
}
