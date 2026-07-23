/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const PanelWorkspaceRegistryEntrySchema = z.object({ "id": z.string(), "path": z.string(), "display_name": z.string().optional(), "pinned": z.boolean().optional(), "registered_at": z.string().datetime({ offset: true }), "last_seen_at": z.string().datetime({ offset: true }).optional() }).strict().describe("One entry in the global Punakawan Panel workspace registry (workspaces.yaml at an OS-specific config path), per punakawan-panel-implementation-plan.md §7. Stores panel discovery metadata only; canonical workspace configuration remains in the workspace's own .punakawan/workspace.yaml.")
export type PanelWorkspaceRegistryEntrySchema = z.infer<typeof PanelWorkspaceRegistryEntrySchema>
