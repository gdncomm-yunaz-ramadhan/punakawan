/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const VerificationDimensionSchema = z.object({ "name": z.enum(["logic","unit","integration","quality","e2e","ci"]), "status": z.enum(["pending","passed","failed"]).describe("pending means no evidence has been recorded for this dimension yet - it is never defaulted to passed or failed without evidence."), "evidence_id": z.string().describe("Id of the EvidenceArtifact backing this dimension's status, if the recording included one.").optional(), "summary": z.string().describe("Short human-readable explanation of the status, if the recording included one.").optional(), "checked_at": z.string().datetime({ offset: true }).describe("When this dimension's status was last recorded or derived.").optional() }).strict().describe("The latest known status of one of the fixed dimensions a lane's attempt is verified against before a review conclusion may rely on it. logic/unit/integration/quality/e2e are recorded directly by whichever role or tool ran that check; ci is either recorded directly or derived from folded CICheck reports for the lane, with an explicit recording always taking precedence over the derived value.")
export type VerificationDimensionSchema = z.infer<typeof VerificationDimensionSchema>
