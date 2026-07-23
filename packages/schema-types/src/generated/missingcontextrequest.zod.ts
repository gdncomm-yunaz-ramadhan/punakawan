/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const MissingContextRequestSchema = z.object({ "id": z.string(), "capsule_id": z.string(), "query": z.string(), "reason": z.string(), "preferred_types": z.array(z.string()).optional(), "blocking": z.boolean(), "status": z.enum(["pending","added_to_revision","rejected","asked_user"]).describe("pending until Semar resolves it. §6.4's four resolutions: search for the context (not itself a terminal status - that's just a search_knowledge call, which then leads to one of the below), add it to a new capsule revision, reject it as irrelevant, or ask the user."), "resolution_note": z.string().optional(), "revised_capsule_id": z.string().describe("Set when status is added_to_revision: the id of the new ContextCapsule (from request_capsule) that supersedes capsule_id with the missing context included.").optional(), "created_at": z.string().datetime({ offset: true }), "resolved_at": z.string().datetime({ offset: true }).optional() }).strict().describe("A subagent's request for context its capsule did not include, routed back to Semar per punakawan-architecture-enhancement-plan.md §6.4. Punakawan never decides how to resolve it (ADR-0016) - it only persists the request and whichever resolution the calling agent, acting as Semar, chooses.")
export type MissingContextRequestSchema = z.infer<typeof MissingContextRequestSchema>
