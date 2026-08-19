/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One continuous multi-project delivery run. Owns zero or more DeliveryLanes and a list of not-yet-routed requirement inputs. State is derived by replaying its DeliveryEvent log, never written directly.
 */
export interface DeliveryOrchestration {
  /**
   * Filesystem-safe ULID (Crockford base32, 26 chars).
   */
  id: string;
  /**
   * Short human-readable summary of what this run delivers, as last set by whoever started or edited it. Absent when nobody supplied one - and absent on every run created before titles existed - so a consumer that needs a label always derives one from the run's requirement references instead of reading this field directly.
   */
  title?: string;
  /**
   * Longer prose about what this run is for and why it exists, as last set by whoever started or edited it. Absent when nobody wrote one; unlike title, nothing derives a substitute, because prose nobody wrote would be invention rather than description.
   */
  description?: string;
  /**
   * Projects explicitly attached to this run, in attachment order. Attachment is a deliberate statement that a run involves a project, kept separate from the incidental fact that some lane happens to name it: a project can be attached before it has any lane, and detaching it never touches lanes it already owns.
   */
  project_ids?: string[];
  /**
   * Id of the knowledge record holding this run's final plan, as persisted by submit_final_plan. Absent until a plan has been recorded against the run.
   */
  plan_record_id?: string;
  /**
   * Id of the workflow run driving this delivery - the same id sessions are identified by everywhere else (a WorkflowRun's id, which is also PanelSessionSummary's id and the run_id the MCP tools thread). Absent when no session has claimed the run.
   */
  session_id?: string;
  status: "pending" | "active" | "cancelled" | "completed";
  /**
   * Requirement sources (Jira/Confluence/GitHub/URL/free-text) not yet routed to a project. Routing and normalization happens elsewhere; this record only persists the raw reference.
   */
  unresolved_inputs: {
    reference: string;
    note?: string;
  }[];
  created_at: string;
  updated_at: string;
  /**
   * Optimistic-concurrency counter; incremented on every applied event.
   */
  revision: number;
  /**
   * Id of a saved workflow definition this run was configured from, if any. When set, its per-role restrictions decide which of the four role stages a lane must complete before its lease can be marked done, in place of the default of requiring all four. Absent when no such definition was attached.
   */
  workflow_definition_id?: string;
}
