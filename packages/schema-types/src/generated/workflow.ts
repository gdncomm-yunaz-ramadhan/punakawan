/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * A running or completed workflow instance and its state-machine position. See punakawan-go-typescript-detailed-plan.md §9, §18.1.
 */
export interface WorkflowRun {
  id: string;
  workspace: string;
  workflow_name:
    "feature-delivery" | "requirement-review" | "browser-flow-capture" | "implementation-only" | "final-review";
  state:
    | "created"
    | "context-building"
    | "awaiting-clarification"
    | "planning"
    | "awaiting-approval"
    | "executing"
    | "reviewing"
    | "blocked"
    | "completed"
    | "failed"
    | "cancelled";
  created_at: string;
  updated_at: string;
  /**
   * Human-readable goal of this run, set by the calling agent at creation or advance time. Used by the panel's session summary (punakawan-panel-implementation-plan.md §8.3); Punakawan never infers or edits this itself.
   */
  objective?: string;
  /**
   * Who or what started this run (e.g. "user", "scheduled", an agent identifier). Set by the calling agent, not inferred.
   */
  initiator?: string;
  /**
   * The Punakawan role currently driving this run, as reported by the calling agent.
   */
  active_role?: "semar" | "gareng" | "petruk" | "bagong";
  checkpoints?: {
    state: string;
    at: string;
    note?: string;
  }[];
  /**
   * Immutable reference to the workflow definition this run was created from (agent-context plan §4.1). Absent for ad hoc runs, which record their path through capability events and the outcome instead of pretending a definition existed. Present for definition-aware runs so the run records the exact id, revision, and content hash that produced it — replacing the old convention of encoding the definition id inside `objective`.
   */
  definition_ref?: {
    id: string;
    revision: number;
    /**
     * sha256:<hex> fingerprint of the definition content at invocation time.
     */
    content_hash: string;
  };
  /**
   * Resolved workflow inputs at invocation time (agent-context plan §4.1/§4.4): declared inputs with caller-supplied values or declared defaults applied. Values are arbitrary JSON.
   */
  inputs?: {
    [k: string]: unknown;
  };
  /**
   * Bounded, immutable snapshot of the context this run was prepared with (agent-context plan §4.1). Holds references and hashes, never a copy of the whole knowledge store. Fully populated by the context preparation service (plan §4.4); at invocation only the `missing` list and revision may be set.
   */
  context_snapshot?: {
    prepared_at?: string;
    /**
     * sha256:<hex> digest covering the definition reference, selected metadata, selected knowledge references and hashes, role-config revision, and inputs.
     */
    digest?: string;
    project_metadata_revision?: number;
    metadata?: {
      key: string;
      value?: unknown;
      reason: string;
    }[];
    knowledge?: {
      id: string;
      content_hash?: string;
      validity?: string;
      reason: string;
    }[];
    /**
     * Context the run needs but could not resolve. A non-empty list drives the run into awaiting-clarification (plan §4.1).
     */
    missing?: {
      /**
       * e.g. "metadata", "knowledge", "input".
       */
      kind: string;
      key?: string;
    }[];
  };
  /**
   * Per-step execution state initialized from the definition's steps at invocation and advanced as steps run (agent-context plan §4.1/§5.3). Absent for ad hoc runs.
   */
  step_progress?: {
    step_id: string;
    state: "ready" | "running" | "done" | "blocked" | "skipped";
    evidence_ids?: string[];
  }[];
  /**
   * The roles.yaml revision in effect when this run was created (plan §50, ROLE-012). Stamped once at creation so a historical run remains reproducible even after the project role configuration is later edited.
   */
  role_config_revision?: number;
  /**
   * Snapshot of the effective role settings (enabled/style/mode/capabilities) for each of the four roles at run-creation time (plan §50, ROLE-012). A map keyed by role name; values are permissive objects so the snapshot stays forward-compatible with future role-config fields.
   */
  effective_role_settings?: {
    [k: string]: {
      [k: string]: unknown;
    };
  };
}
