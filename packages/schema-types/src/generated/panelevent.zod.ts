/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const PanelEventSchema = z.object({ "id": z.string(), "type": z.enum(["system.ready","system.warning","workspace.registered","workspace.updated","workspace.availability_changed","session.started","session.phase_changed","session.progress","session.completed","session.failed","task.created","task.updated","task.blocked","task.completed","knowledge.created","knowledge.updated","knowledge.superseded","approval.requested","approval.resolved","evidence.created","git.state_changed","adapter.health_changed"]), "occurred_at": z.string().datetime({ offset: true }), "workspace_id": z.string().optional(), "session_id": z.string().optional(), "entity_id": z.string().optional(), "revision": z.number().int().gte(0).optional(), "payload": z.record(z.string(), z.any()).optional() }).strict().describe("SSE envelope pushed to the Punakawan Panel frontend at GET /api/v1/events, per punakawan-panel-implementation-plan.md §12. Distinct from Event (event.schema.json), which is the per-run execution journal this envelope is often derived from.")
export type PanelEventSchema = z.infer<typeof PanelEventSchema>
