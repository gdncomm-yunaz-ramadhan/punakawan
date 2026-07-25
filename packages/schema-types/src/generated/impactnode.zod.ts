/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const ImpactNodeSchema = z.object({ "id": z.string().describe("Stable typed id, e.g. api:affiliate-api:getMerchantBadge or config:affiliate-api:payout.retry.max_attempts."), "type": z.enum(["project","repository","source_symbol","api_operation","configuration_key","database_object","test","deployment_artifact","workflow","knowledge_record","plan","task","external_issue","team_owner"]), "label": z.string().describe("Human-readable name for the node.").optional(), "repository": z.string().describe("Repository this node belongs to, when applicable.").optional(), "attributes": z.record(z.string(), z.any()).describe("Free-form typed attributes (file path, owner team, operation id, ...). Kept generic so builders need no per-type node subclass.").optional() }).strict().describe("One node in a project's Cross-Repository Impact Graph, per punakawan-role-config-distinguished-improvements-plan.md Part III §24. Nodes are the things a change can affect (symbols, API operations, config keys, tests, deployments, ...). Persisted append-only to .punakawan/impact/nodes.jsonl; id is stable and typed so builders can upsert idempotently.")
export type ImpactNodeSchema = z.infer<typeof ImpactNodeSchema>
