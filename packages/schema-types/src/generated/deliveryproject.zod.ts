/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const DeliveryProjectSchema = z.object({ "id": z.string().describe("Filesystem-safe ULID (Crockford base32, 26 chars)."), "slug": z.string().describe("Short, filesystem-safe, human-chosen project identifier used in worktree and lane paths."), "repository_url": z.string(), "default_branch": z.string().optional(), "status": z.enum(["active","disabled"]), "registered_at": z.string().datetime({ offset: true }), "revision": z.number().int().gte(0).describe("Optimistic-concurrency counter; incremented on every update.") }).strict().describe("A registered project binding in the canonical multi-project delivery control plane. One row per project the orchestrator is allowed to route work to; unknown project ids are never inferred from ambient working-directory scope.")
export type DeliveryProjectSchema = z.infer<typeof DeliveryProjectSchema>
