/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const DeliveryOrchestrationSchema = z.object({ "id": z.string().describe("Filesystem-safe ULID (Crockford base32, 26 chars)."), "status": z.enum(["pending","active","cancelled","completed"]), "unresolved_inputs": z.array(z.object({ "reference": z.string(), "note": z.string().optional() }).strict()).describe("Requirement sources (Jira/Confluence/GitHub/URL/free-text) not yet routed to a project. Routing and normalization is punokawan-14yn.2's concern; this task only persists the raw reference."), "created_at": z.string().datetime({ offset: true }), "updated_at": z.string().datetime({ offset: true }), "revision": z.number().int().gte(0).describe("Optimistic-concurrency counter; incremented on every applied event."), "workflow_definition_id": z.string().describe("Id of a saved workflow definition this run was configured from, if any. When set, its per-role restrictions decide which of the four role stages a lane must complete before its lease can be marked done, in place of the default of requiring all four. Absent when no such definition was attached.").optional() }).strict().describe("One continuous multi-project delivery run (punokawan-14yn.1). Owns zero or more DeliveryLanes and a list of not-yet-routed requirement inputs. State is derived by replaying its DeliveryEvent log, never written directly. See affiliate-platform-delivery-feedback-2026-08-07.md.")
export type DeliveryOrchestrationSchema = z.infer<typeof DeliveryOrchestrationSchema>
