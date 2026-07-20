/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * Structured event record written to the JSONL event journal. See punakawan-go-typescript-detailed-plan.md §7.5, §19.1.
 */
export interface Event {
  id: string;
  type: string;
  timestamp: string;
  run_id: string;
  workflow?: string;
  role?: "semar" | "gareng" | "petruk" | "bagong";
  workspace?: string;
  repository?: string;
  task?: string;
  adapter?: string;
  tool?: string;
  operation: string;
  duration_ms?: number;
  result: "success" | "failure" | "cancelled" | "timeout";
  approval_id?: string;
  payload?: {
    [k: string]: unknown;
  };
}
