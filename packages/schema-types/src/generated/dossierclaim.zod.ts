/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const DossierClaimSchema = z.object({ "id": z.string(), "dossier_id": z.string().optional(), "type": z.string().describe("e.g. compatibility, implementation, risk, completeness."), "statement": z.string(), "producer": z.object({ "role": z.enum(["semar","gareng","petruk","bagong"]) }).strict(), "status": z.enum(["draft","claimed","supported","verified","disputed","rejected","superseded"]).describe("The §2.3 evidence ladder."), "evidence": z.array(z.string()).describe("DossierEvidence ids supporting the claim.").optional(), "verification": z.object({ "role": z.enum(["semar","gareng","petruk","bagong"]).optional(), "result": z.enum(["verified","disputed"]).optional(), "note": z.string().optional(), "at": z.string().datetime({ offset: true }).optional() }).strict().optional() }).strict().describe("One claim within a Change Dossier, per punakawan-role-config-distinguished-improvements-plan.md §34. A claim has a producer role and a status in the evidence ladder; a role can never verify its own claim (enforced by the store): Petruk produces implementation claims, Gareng risk/feasibility, Semar completeness/coordination, and Bagong verifies or disputes.")
export type DossierClaimSchema = z.infer<typeof DossierClaimSchema>
