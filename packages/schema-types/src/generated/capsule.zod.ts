/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const ContextCapsuleSchema = z.object({ "id": z.string(), "digest": z.string().regex(new RegExp("^sha256:[0-9a-f]{64}$")), "task_id": z.string(), "created_at": z.string().datetime({ offset: true }), "role": z.enum(["gareng","petruk","bagong"]), "objective": z.string(), "requirements": z.array(z.object({ "id": z.string(), "summary": z.string().optional() }).strict()).optional(), "acceptance_criteria": z.array(z.string()).optional(), "constraints": z.array(z.string()).optional(), "relevant_knowledge": z.array(z.object({ "id": z.string(), "summary": z.string().optional(), "reason": z.string().describe("Why this item was selected for this capsule, e.g. a search_knowledge match explanation. Set only when the item came from Semar's automatic knowledge-retrieval pipeline (AEP-M7); manually-cited items may omit it. See punakawan-architecture-enhancement-plan.md §11.13.").optional() }).strict()).optional(), "evidence": z.array(z.object({ "id": z.string(), "summary": z.string().optional() }).strict()).optional(), "assumptions": z.array(z.string()).optional(), "unresolved_questions": z.array(z.string()).optional(), "allowed_tools": z.array(z.string()), "forbidden_actions": z.array(z.string()), "expected_output": z.string().optional(), "token_budget": z.number().int().gte(1).optional() }).strict().describe("An immutable, hashable, bounded context handed to one Gareng/Petruk/Bagong invocation. See punakawan-architecture-enhancement-plan.md §6 and AEP-M1 (punokawan-0m9). Digest is computed over role/objective/requirements/acceptance_criteria/constraints/relevant_knowledge/evidence/allowed_tools/forbidden_actions per §6.3 and must be recomputed (not mutated in place) if any of those fields change.")
export type ContextCapsuleSchema = z.infer<typeof ContextCapsuleSchema>
