/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One node in a project's Cross-Repository Impact Graph, per punakawan-role-config-distinguished-improvements-plan.md Part III §24. Nodes are the things a change can affect (symbols, API operations, config keys, tests, deployments, ...). Persisted append-only to .punakawan/impact/nodes.jsonl; id is stable and typed so builders can upsert idempotently.
 */
export interface ImpactNode {
  /**
   * Stable typed id, e.g. api:affiliate-api:getMerchantBadge or config:affiliate-api:payout.retry.max_attempts.
   */
  id: string;
  type:
    | "project"
    | "repository"
    | "source_symbol"
    | "api_operation"
    | "configuration_key"
    | "database_object"
    | "test"
    | "deployment_artifact"
    | "workflow"
    | "knowledge_record"
    | "plan"
    | "task"
    | "external_issue"
    | "team_owner";
  /**
   * Human-readable name for the node.
   */
  label?: string;
  /**
   * Repository this node belongs to, when applicable.
   */
  repository?: string;
  /**
   * Free-form typed attributes (file path, owner team, operation id, ...). Kept generic so builders need no per-type node subclass.
   */
  attributes?: {
    [k: string]: unknown;
  };
}
