/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * Health/availability of one data source (Dolt, BD, Git, an adapter, ...) backing a workspace's panel view, per punakawan-panel-implementation-plan.md §9's WorkspaceAvailability enum. Failure of one source must not fail the whole workspace view.
 */
export interface PanelSourceHealth {
  source: string;
  availability: "available" | "partially_available" | "busy" | "unavailable" | "invalid";
  message?: string;
  checked_at: string;
}
