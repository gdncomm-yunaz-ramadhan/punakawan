/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const NeedUserInputSchema = z.object({ "kind": z.enum(["missing_context","decision_required"]), "question": z.string(), "missing_fields": z.array(z.string()).optional(), "options": z.array(z.object({ "id": z.string(), "label": z.string(), "impact": z.string() }).strict()).optional() }).strict().describe("A direct, non-persisted clarification result returned instead of a mutation when required context is missing/contradictory or a material decision has more than one defensible answer. See the improvement plan's Delivery contract and Public result semantics.")
export type NeedUserInputSchema = z.infer<typeof NeedUserInputSchema>
