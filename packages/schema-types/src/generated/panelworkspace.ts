/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One entry in the global Punakawan Panel workspace registry (workspaces.yaml at an OS-specific config path), per punakawan-panel-implementation-plan.md §7. Stores panel discovery metadata only; canonical workspace configuration remains in the workspace's own .punakawan/workspace.yaml.
 */
export interface PanelWorkspaceRegistryEntry {
  id: string;
  path: string;
  display_name?: string;
  pinned?: boolean;
  registered_at: string;
  last_seen_at?: string;
}
