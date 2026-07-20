/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const ApprovalRecordSchema = z.object({ "id": z.string(), "run_id": z.string(), "operation": z.enum(["external-write","git-push","pull-request-creation","issue-creation","issue-transition","confluence-update","existing-browser-session-access","secret-access","network-host-expansion","destructive-filesystem-action","deployment-action"]), "target": z.string().optional(), "reason": z.string().optional(), "requested_by": z.enum(["semar","gareng","petruk","bagong"]), "preview": z.string().optional(), "status": z.enum(["pending","approved","denied"]), "policy_level": z.enum(["deny","require-approval","allow","allow-with-constraints"]).optional(), "approved_by": z.string().optional(), "created_at": z.string().datetime({ offset: true }), "resolved_at": z.string().datetime({ offset: true }).optional() }).strict().describe("A recorded approval decision for a policy-gated operation. See punakawan-go-typescript-detailed-plan.md §16.")
export type ApprovalRecordSchema = z.infer<typeof ApprovalRecordSchema>
