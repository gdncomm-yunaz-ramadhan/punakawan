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
