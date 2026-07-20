/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * A recorded approval decision for a policy-gated operation. See punakawan-go-typescript-detailed-plan.md §16.
 */
export interface ApprovalRecord {
  id: string;
  run_id: string;
  operation:
    | "external-write"
    | "git-push"
    | "pull-request-creation"
    | "issue-creation"
    | "issue-transition"
    | "confluence-update"
    | "existing-browser-session-access"
    | "secret-access"
    | "network-host-expansion"
    | "destructive-filesystem-action"
    | "deployment-action";
  target?: string;
  reason?: string;
  requested_by: "semar" | "gareng" | "petruk" | "bagong";
  preview?: string;
  status: "pending" | "approved" | "denied";
  policy_level?: "deny" | "require-approval" | "allow" | "allow-with-constraints";
  approved_by?: string;
  created_at: string;
  resolved_at?: string;
}
