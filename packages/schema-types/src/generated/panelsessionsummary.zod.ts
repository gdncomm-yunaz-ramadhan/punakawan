/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const PanelSessionSummarySchema = z.object({ "id": z.string(), "workspace_id": z.string(), "workflow": z.string(), "status": z.string(), "started_at": z.string().datetime({ offset: true }), "updated_at": z.string().datetime({ offset: true }), "initiator": z.string().optional(), "objective": z.string().optional(), "active_role": z.enum(["semar","gareng","petruk","bagong"]).optional(), "task_counts": z.object({ "total": z.number().int().gte(0).optional(), "open": z.number().int().gte(0).optional(), "in_progress": z.number().int().gte(0).optional(), "blocked": z.number().int().gte(0).optional(), "closed": z.number().int().gte(0).optional() }).strict().describe("Best-effort workspace-wide bd task counts as of this checkpoint. Not scoped to this run: bd issues are not tagged per-run today.").optional(), "evidence_count": z.number().int().gte(0).optional(), "warning_count": z.number().int().gte(0).optional(), "error_count": z.number().int().gte(0).optional() }).strict().describe("Compact per-run summary written to .punakawan/runs/<run-id>/summary.yaml as part of normal run checkpointing, per punakawan-panel-implementation-plan.md §8.3. Backs the panel's session list/overview and CLI recovery inspection; not panel-specific persistence.")
export type PanelSessionSummarySchema = z.infer<typeof PanelSessionSummarySchema>
