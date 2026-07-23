/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const ArtifactCommentSchema = z.object({ "id": z.string(), "review_id": z.string(), "author": z.string(), "status": z.enum(["open","addressed","partially_addressed","rejected_by_agent","needs_clarification","obsolete","resolved_by_user"]), "anchor": z.object({ "kind": z.literal("markdown_block"), "block_id": z.string().optional(), "heading_path": z.array(z.string()).optional(), "base_revision_hash": z.string().regex(new RegExp("^sha256:[0-9a-f]{64}$")), "quoted_text": z.string().optional() }).strict(), "body": z.string() }).strict().describe("One anchored review comment. See punakawan-artifact-review-plan-mutation-plan-v2.md §5.2/§6. anchor.block_id/base_revision_hash/heading_path/quoted_text together drive the 5-step anchor resolution order (§6): exact block ID, exact content hash, heading path plus quoted text, heading path plus fuzzy quoted text, or conflicted.")
export type ArtifactCommentSchema = z.infer<typeof ArtifactCommentSchema>
