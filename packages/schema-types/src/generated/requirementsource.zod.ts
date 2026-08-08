/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const RequirementSourceSchema = z.object({ "id": z.string().describe("Filesystem-safe ULID (Crockford base32, 26 chars)."), "orchestration_id": z.string(), "provider": z.enum(["jira","confluence","github","url","freetext"]), "external_id": z.string().describe("The provider's own identifier (issue key, page id, issue/PR number); absent for freetext.").optional(), "canonical_key": z.string().describe("Exact dedup/pin key, e.g. \"jira:PAY-1842\" or \"url:https://example.com/doc\". Never derived from fuzzy text similarity."), "content_hash": z.string().regex(new RegExp("^sha256:[0-9a-f]{64}$")), "title": z.string(), "summary": z.string().optional(), "parent_source_id": z.string().describe("Set when this source is a Jira/GitHub subtask resolved to its source parent; the referenced RequirementSource must already exist in the same orchestration.").optional(), "captured_at": z.string().datetime({ offset: true }), "revision": z.number().int().gte(0).describe("Optimistic-concurrency counter; incremented on every applied event.") }).strict().describe("An immutable, canonicalized snapshot of one requirement input (Jira, Confluence, GitHub, a document URL, or free text) captured into a DeliveryOrchestration (punokawan-14yn.2). canonical_key is an exact, provider-specific identifier (never a fuzzy/similar-wording match), so a pinned requirement can never be silently replaced by a similar retrieved result. Grouped into a ParentTask once routing/decomposition decides which task it belongs to. See affiliate-platform-delivery-feedback-2026-08-07.md.")
export type RequirementSourceSchema = z.infer<typeof RequirementSourceSchema>
