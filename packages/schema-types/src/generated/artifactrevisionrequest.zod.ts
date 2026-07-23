/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const ArtifactRevisionRequestSchema = z.object({ "metadata": z.object({ "id": z.string(), "review_id": z.string(), "submitted_at": z.string().datetime({ offset: true }), "submitted_by": z.string() }).strict(), "base_artifact": z.object({ "type": z.enum(["plan","retrieval_recipe"]), "id": z.string(), "version": z.number().int().gte(1), "revision_hash": z.string().regex(new RegExp("^sha256:[0-9a-f]{64}$")) }).strict(), "workflow": z.object({ "type": z.enum(["revise_plan_from_review","revise_retrieval_recipe_from_review"]), "auto_start": z.boolean().optional(), "require_final_acceptance": z.boolean().optional(), "max_revision_attempts": z.number().int().gte(1).optional() }).strict(), "comments": z.object({ "snapshot_hash": z.string().regex(new RegExp("^sha256:[0-9a-f]{64}$")), "count": z.number().int().gte(0) }).strict() }).strict().describe("An immutable snapshot created when a review is submitted for revision. See punakawan-artifact-review-plan-mutation-plan-v2.md §5.3/§8. comments.snapshot_hash freezes the comment set at submission time - later comment edits do not retroactively change what an in-flight revision run is working from.")
export type ArtifactRevisionRequestSchema = z.infer<typeof ArtifactRevisionRequestSchema>
