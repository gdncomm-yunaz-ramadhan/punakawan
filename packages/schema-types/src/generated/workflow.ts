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
  checkpoints?: {
    state: string;
    at: string;
    note?: string;
  }[];
}
