/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const ArtifactCommentSchema = z.object({ "id": z.string(), "review_id": z.string(), "author": z.string(), "status": z.enum(["open","addressed","partially_addressed","rejected_by_agent","needs_clarification","obsolete","resolved_by_user"]), "anchor": z.object({ "kind": z.enum(["markdown_block","recipe_field_path"]), "block_id": z.string().optional(), "heading_path": z.array(z.string()).optional(), "base_revision_hash": z.string().regex(new RegExp("^sha256:[0-9a-f]{64}$")), "quoted_text": z.string().optional(), "field_path": z.string().describe("recipe_field_path anchors only: a dotted path (gjson syntax: object keys and array indices both separated by \".\", e.g. \"selector.all.0.value\" or \"inputs.1.required\") into the recipe's canonical JSON serialization. Resolved by exact match against the reviewed revision's serialized content - see internal/artifact's recipe field-path resolver.").optional() }).strict(), "body": z.string() }).strict().describe("One anchored review comment. See punakawan-artifact-review-plan-mutation-plan-v2.md §5.2/§6. For a markdown_block anchor, block_id/base_revision_hash/heading_path/quoted_text together drive the 5-step anchor resolution order (§6): exact block ID, exact content hash, heading path plus quoted text, heading path plus fuzzy quoted text, or conflicted. For a recipe_field_path anchor (punakawan-procedural-knowledge-retrieval-recipe-plan-final.md Phase 5), field_path plus base_revision_hash locate an exact field/condition in a retrieval_recipe's structured content instead: structured data either has the exact path against the reviewed revision or it does not, so no fuzzy fallback chain applies.")
export type ArtifactCommentSchema = z.infer<typeof ArtifactCommentSchema>
