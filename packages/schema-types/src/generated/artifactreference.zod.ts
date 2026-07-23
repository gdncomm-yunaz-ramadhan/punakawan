/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const ArtifactReferenceSchema = z.object({ "type": z.enum(["plan","retrieval_recipe"]).describe("Artifact type. Only \"plan\" is implemented by this plan; \"retrieval_recipe\" is reserved for punakawan-procedural-knowledge-retrieval-recipe-plan-final.md's own review/mutation reuse once its compiler and validation lifecycle exist (§4), not enabled here."), "id": z.string(), "version": z.number().int().gte(1), "revision_hash": z.string().regex(new RegExp("^sha256:[0-9a-f]{64}$")), "workspace_id": z.string(), "format": z.enum(["markdown","json"]).describe("Content encoding of the version this reference points at. \"markdown\" is a plan's format. \"json\" is a retrieval_recipe's canonical serialization: indented JSON matching `punakawan knowledge recipe show`'s existing rendering, chosen so the artifact-review diff generator's line-based LCS diff (internal/artifact/diff.go) produces a stable, human-readable comparison without inventing a second recipe text format."), "canonical_location": z.string().describe("Path to this version's immutable content, e.g. .punakawan/plans/<id>/versions/<version>.md (§7). Absent for a retrieval_recipe artifact, whose canonical version lives in durable knowledge instead (§7).").optional() }).strict().describe("Points at one immutable version of a reviewable artifact. See punakawan-artifact-review-plan-mutation-plan-v2.md §4. Every artifact type this record can point at must provide a version reader, stable anchor resolver, proposal renderer, diff generator, validator, and acceptance handler (§4) - this schema only carries the pointer, not that behavior.")
export type ArtifactReferenceSchema = z.infer<typeof ArtifactReferenceSchema>
