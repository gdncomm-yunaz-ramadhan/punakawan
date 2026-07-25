/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const ImpactEdgeSchema = z.object({ "from": z.string().describe("Source node id."), "to": z.string().describe("Target node id."), "type": z.enum(["contains","defines","calls","implements","consumes","tests","configures","deploys","depends_on","documented_by","owned_by","tracked_by","contradicts","derived_from"]), "confidence": z.enum(["observed","inferred","verified","disputed"]).describe("observed: directly seen in a source. inferred: derived heuristically. verified: independently confirmed. disputed: a contradiction exists about this edge."), "evidence": z.array(z.object({ "type": z.string().describe("e.g. source_location, openapi_reference, test_reference."), "ref": z.string().describe("e.g. src/lib/api/merchant.ts:44.").optional() }).strict()).optional(), "discovered_by": z.object({ "role": z.string().optional(), "method": z.string().describe("e.g. openapi-client-reference.").optional() }).strict().optional() }).strict().describe("One evidence-backed directed edge in a project's Cross-Repository Impact Graph, per punakawan-role-config-distinguished-improvements-plan.md Part III §25/§26. Edges connect two ImpactNode ids and carry a confidence and the evidence that discovered them, so an impact query can distinguish observed facts from inferences. Persisted append-only to .punakawan/impact/edges.jsonl.")
export type ImpactEdgeSchema = z.infer<typeof ImpactEdgeSchema>
