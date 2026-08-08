/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const DeliveryLaneSchema = z.object({ "id": z.string().describe("Filesystem-safe ULID (Crockford base32, 26 chars)."), "orchestration_id": z.string(), "project_id": z.string(), "parent_task_id": z.string().describe("Id of the parent task this lane delivers, assigned once punokawan-14yn.2's dependency graph resolves it. Empty until then.").optional(), "status": z.enum(["pending","active","cancelled","completed","failed"]), "created_at": z.string().datetime({ offset: true }), "updated_at": z.string().datetime({ offset: true }), "revision": z.number().int().gte(0).describe("Optimistic-concurrency counter; incremented on every applied event.") }).strict().describe("One project's independent delivery lane within a DeliveryOrchestration (punokawan-14yn.1). A lane always carries its own project_id so cross-project reads and writes fail closed. Worktree lifecycle, worker scheduling, and role execution are later tasks (punokawan-14yn.3/5); this task only defines and persists lane identity and status. See affiliate-platform-delivery-feedback-2026-08-07.md.")
export type DeliveryLaneSchema = z.infer<typeof DeliveryLaneSchema>
