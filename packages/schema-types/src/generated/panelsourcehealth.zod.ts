/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const PanelSourceHealthSchema = z.object({ "source": z.string(), "availability": z.enum(["available","partially_available","busy","unavailable","invalid"]), "message": z.string().optional(), "checked_at": z.string().datetime({ offset: true }) }).strict().describe("Health/availability of one data source (Dolt, BD, Git, an adapter, ...) backing a workspace's panel view, per punakawan-panel-implementation-plan.md §9's WorkspaceAvailability enum. Failure of one source must not fail the whole workspace view.")
export type PanelSourceHealthSchema = z.infer<typeof PanelSourceHealthSchema>
