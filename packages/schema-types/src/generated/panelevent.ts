/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * SSE envelope pushed to the Punakawan Panel frontend at GET /api/v1/events, per punakawan-panel-implementation-plan.md §12. Distinct from Event (event.schema.json), which is the per-run execution journal this envelope is often derived from.
 */
export interface PanelEvent {
  id: string;
  type:
    | "system.ready"
    | "system.warning"
    | "workspace.registered"
    | "workspace.updated"
    | "workspace.availability_changed"
    | "session.started"
    | "session.phase_changed"
    | "session.progress"
    | "session.completed"
    | "session.failed"
    | "task.created"
    | "task.updated"
    | "task.blocked"
    | "task.completed"
    | "knowledge.created"
    | "knowledge.updated"
    | "knowledge.superseded"
    | "approval.requested"
    | "approval.resolved"
    | "evidence.created"
    | "git.state_changed"
    | "adapter.health_changed";
  occurred_at: string;
  workspace_id?: string;
  session_id?: string;
  entity_id?: string;
  revision?: number;
  payload?: {
    [k: string]: unknown;
  };
}
