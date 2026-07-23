/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * Compact per-run summary written to .punakawan/runs/<run-id>/summary.yaml as part of normal run checkpointing, per punakawan-panel-implementation-plan.md §8.3. Backs the panel's session list/overview and CLI recovery inspection; not panel-specific persistence.
 */
export interface PanelSessionSummary {
  id: string;
  workspace_id: string;
  workflow: string;
  status: string;
  started_at: string;
  updated_at: string;
  initiator?: string;
  objective?: string;
  active_role?: "semar" | "gareng" | "petruk" | "bagong";
  /**
   * Best-effort workspace-wide bd task counts as of this checkpoint. Not scoped to this run: bd issues are not tagged per-run today.
   */
  task_counts?: {
    total?: number;
    open?: number;
    in_progress?: number;
    blocked?: number;
    closed?: number;
  };
  evidence_count?: number;
  warning_count?: number;
  error_count?: number;
}
