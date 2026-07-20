/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const EventSchema = z.object({ "id": z.string(), "type": z.string(), "timestamp": z.string().datetime({ offset: true }), "run_id": z.string(), "workflow": z.string().optional(), "role": z.enum(["semar","gareng","petruk","bagong"]).optional(), "workspace": z.string().optional(), "repository": z.string().optional(), "task": z.string().optional(), "adapter": z.string().optional(), "tool": z.string().optional(), "operation": z.string(), "duration_ms": z.number().int().gte(0).optional(), "result": z.enum(["success","failure","cancelled","timeout"]), "approval_id": z.string().optional(), "payload": z.record(z.string(), z.any()).optional() }).strict().describe("Structured event record written to the JSONL event journal. See punakawan-go-typescript-detailed-plan.md §7.5, §19.1.")
export type EventSchema = z.infer<typeof EventSchema>
