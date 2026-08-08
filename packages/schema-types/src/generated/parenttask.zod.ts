/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const ParentTaskSchema = z.object({ "id": z.string().describe("Filesystem-safe ULID (Crockford base32, 26 chars)."), "orchestration_id": z.string(), "project_id": z.string().describe("Set once routing resolves this task to a project; absent while unrouted.").optional(), "title": z.string(), "source_ids": z.array(z.string()).describe("RequirementSource ids grouped into this task."), "status": z.enum(["unrouted","routed","cancelled"]), "created_at": z.string().datetime({ offset: true }), "updated_at": z.string().datetime({ offset: true }), "revision": z.number().int().gte(0).describe("Optimistic-concurrency counter; incremented on every applied event.") }).strict().describe("One node in an orchestration's task dependency graph (punokawan-14yn.2): a group of one or more RequirementSources routed, or awaiting routing, to a single project. A DeliveryLane (punokawan-14yn.1) is created once a ParentTask is routed and its DAG position allows execution to begin - lane/worker execution status is not duplicated here. See affiliate-platform-delivery-feedback-2026-08-07.md.")
export type ParentTaskSchema = z.infer<typeof ParentTaskSchema>
