/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const DeliveryEventSchema = z.object({ "id": z.string().describe("Filesystem-safe ULID (Crockford base32, 26 chars)."), "orchestration_id": z.string(), "lane_id": z.string().describe("Set for lane-scoped events; absent for orchestration-scoped events.").optional(), "idempotency_key": z.string(), "type": z.enum(["orchestration.created","orchestration.cancelled","orchestration.completed","input.registered","input.resolved","lane.created","lane.status_changed"]), "payload": z.record(z.string(), z.any()), "sequence": z.number().int().gte(0).describe("Monotonic per orchestration_id; determines deterministic replay order."), "occurred_at": z.string().datetime({ offset: true }) }).strict().describe("One append-only, idempotent state-transition event for a DeliveryOrchestration or one of its DeliveryLanes (punokawan-14yn.1). Orchestration and lane state is derived by replaying these in sequence order; the same idempotency_key is applied at most once. See affiliate-platform-delivery-feedback-2026-08-07.md.")
export type DeliveryEventSchema = z.infer<typeof DeliveryEventSchema>
