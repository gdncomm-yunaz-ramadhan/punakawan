/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const DependencyEdgeSchema = z.object({ "id": z.string().describe("Filesystem-safe ULID (Crockford base32, 26 chars)."), "orchestration_id": z.string(), "from_task_id": z.string(), "to_task_id": z.string(), "type": z.enum(["requires","produces-input-for","serializes-with","informational"]), "evidence": z.string().describe("Why this edge exists: an explicit source link, a repository fact, or a model-inference rationale.").optional(), "origin": z.enum(["explicit-source","user","repository-fact","model-inference"]), "confidence": z.number().gte(0).lte(1), "status": z.enum(["active","removed"]), "removal_evidence": z.string().describe("Required to remove an edge from a task that has already been routed; not required before routing.").optional(), "created_at": z.string().datetime({ offset: true }), "revision": z.number().int().gte(0).describe("Optimistic-concurrency counter; incremented on every applied event.") }).strict().describe("One typed edge in an orchestration's task dependency graph (punokawan-14yn.2), from_task_id -> to_task_id meaning from_task_id depends on to_task_id. Only \"requires\" and \"produces-input-for\" block execution; \"serializes-with\" and \"informational\" are non-blocking. Explicit source/user origins outrank repository-fact, which outranks model-inference. See affiliate-platform-delivery-feedback-2026-08-07.md.")
export type DependencyEdgeSchema = z.infer<typeof DependencyEdgeSchema>
