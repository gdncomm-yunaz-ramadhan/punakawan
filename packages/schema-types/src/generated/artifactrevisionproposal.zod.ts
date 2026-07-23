/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const ArtifactRevisionProposalSchema = z.object({ "metadata": z.object({ "id": z.string(), "review_id": z.string(), "revision_request_id": z.string(), "attempt": z.number().int().gte(1), "status": z.enum(["pending","ready","failed","superseded"]) }).strict(), "base": z.object({ "artifact_id": z.string(), "version": z.number().int().gte(1), "revision_hash": z.string().regex(new RegExp("^sha256:[0-9a-f]{64}$")) }).strict(), "proposed": z.object({ "version": z.number().int().gte(1), "content_hash": z.string().regex(new RegExp("^sha256:[0-9a-f]{64}$")), "content_location": z.string() }).strict(), "results": z.object({ "addressed_comments": z.number().int().gte(0).optional(), "partially_addressed_comments": z.number().int().gte(0).optional(), "unresolved_comments": z.number().int().gte(0).optional(), "validation_status": z.enum(["pending","passed","failed"]).optional() }).strict().optional() }).strict().describe("One revision attempt's result, not yet a new canonical version. See punakawan-artifact-review-plan-mutation-plan-v2.md §5.4/§9. base.revision_hash is compared against the artifact's current canonical hash at acceptance time (§12); a mismatch means the review is conflicted, never a silent overwrite.")
export type ArtifactRevisionProposalSchema = z.infer<typeof ArtifactRevisionProposalSchema>
