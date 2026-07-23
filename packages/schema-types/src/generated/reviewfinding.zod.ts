/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const ReviewFindingSchema = z.object({ "id": z.string(), "severity": z.enum(["blocker","major","minor","suggestion"]), "category": z.string(), "title": z.string(), "explanation": z.string(), "file": z.string().optional(), "start_line": z.number().int().optional(), "end_line": z.number().int().optional(), "evidence": z.array(z.object({ "id": z.string(), "summary": z.string().optional() }).strict()), "related_knowledge": z.array(z.object({ "id": z.string(), "summary": z.string().optional() }).strict()), "suggested_fix": z.string().optional(), "confidence": z.number().gte(0).lte(1) }).strict().describe("One deduplicated, prioritized finding from review_pr's pipeline: Gareng/Petruk build independent review capsules, Bagong verifies findings against the diff and evidence, Semar deduplicates and prioritizes. See punakawan-architecture-enhancement-plan.md §8.2.")
export type ReviewFindingSchema = z.infer<typeof ReviewFindingSchema>
