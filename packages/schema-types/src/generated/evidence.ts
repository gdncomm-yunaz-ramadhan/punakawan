/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * A single piece of evidence linked to a task run and its supporting artifact. See punakawan-go-typescript-detailed-plan.md §17.
 */
export interface EvidenceRecord {
  id: string;
  run_id: string;
  task_id?: string;
  type:
    | "source-excerpt"
    | "repository-snapshot"
    | "command-output"
    | "test-report"
    | "playwright-trace"
    | "screenshot"
    | "api-diff"
    | "git-diff"
    | "commit"
    | "user-answer"
    | "approval-record"
    | "external-response"
    | "review-finding";
  path?: string;
  content_hash?: string;
  summary?: string;
  created_at: string;
}
