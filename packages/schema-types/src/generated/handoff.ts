/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * A compact, resumable snapshot of in-progress work, per punakawan-role-config-distinguished-improvements-plan.md Part V §41. It lets work continue across agent clients, model providers, sessions, machines, and people WITHOUT depending on conversation transcript history. It references existing objects (plan, tasks, contradictions, evidence, dossier) by id rather than copying them, so it stays small.
 */
export interface HandoffCapsule {
  version: "punakawan.handoff/v1";
  id: string;
  project_id: string;
  run_id: string;
  created_at?: string;
  /**
   * A superseded capsule must not resume silently (§43/acceptance).
   */
  superseded?: boolean;
  objective: {
    statement: string;
    source_refs?: string[];
  };
  /**
   * e.g. implementation, verification.
   */
  current_phase: string;
  accepted_plan?: {
    id?: string;
    version?: number;
  };
  role_configuration_revision?: number;
  completed_tasks?: string[];
  current_task?: {
    id?: string;
    next_action?: string;
  };
  changed_repositories?: string[];
  open_contradictions?: string[];
  unresolved_risks?: string[];
  impact_summary?: {
    required_repositories?: string[];
    excluded_repositories?: string[];
  };
  dossier?: {
    id?: string;
    status?: string;
  };
  evidence?: string[];
  created_by?: {
    role?: string;
    agent_client?: string;
  };
}
