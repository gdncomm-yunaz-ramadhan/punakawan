/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const ArtifactReviewSchema = z.object({ "metadata": z.object({ "id": z.string(), "workspace_id": z.string(), "status": z.enum(["draft","submitted","queued","revising","awaiting_clarification","proposal_ready","revision_requested","accepted","rejected","cancelled","failed","conflicted"]), "created_by": z.string(), "created_at": z.string().datetime({ offset: true }), "updated_at": z.string().datetime({ offset: true }).optional() }).strict(), "artifact": z.object({ "type": z.enum(["plan","retrieval_recipe"]), "id": z.string(), "version": z.number().int().gte(1), "revision_hash": z.string().regex(new RegExp("^sha256:[0-9a-f]{64}$")) }).strict(), "review": z.object({ "title": z.string(), "instruction": z.string().optional(), "comment_count": z.number().int().gte(0).optional() }).strict() }).strict().describe("A review session against one artifact version. See punakawan-artifact-review-plan-mutation-plan-v2.md §5.1.")
export type ArtifactReviewSchema = z.infer<typeof ArtifactReviewSchema>
