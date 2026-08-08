/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const DeliveryEventSchema = z.object({ "id": z.string().describe("Filesystem-safe ULID (Crockford base32, 26 chars)."), "orchestration_id": z.string(), "entity_id": z.string().describe("Set for events scoped to one lane, requirement source, parent task, or dependency edge; absent for orchestration-scoped events.").optional(), "idempotency_key": z.string(), "type": z.enum(["orchestration.created","orchestration.cancelled","orchestration.completed","input.registered","input.resolved","lane.created","lane.status_changed","requirement.captured","task.created","task.routed","edge.added","edge.removed"]), "payload": z.record(z.string(), z.any()), "sequence": z.number().int().gte(0).describe("Monotonic per orchestration_id; determines deterministic replay order."), "occurred_at": z.string().datetime({ offset: true }) }).strict().describe("One append-only, idempotent state-transition event for a DeliveryOrchestration or one of its scoped sub-entities (lane, requirement source, parent task, dependency edge - punokawan-14yn.1/.2). Orchestration and sub-entity state is derived by replaying these in sequence order; the same idempotency_key is applied at most once. See affiliate-platform-delivery-feedback-2026-08-07.md.")
export type DeliveryEventSchema = z.infer<typeof DeliveryEventSchema>
