/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One typed edge in an orchestration's task dependency graph, from_task_id -> to_task_id meaning from_task_id depends on to_task_id. Only "requires" and "produces-input-for" block execution; "serializes-with" and "informational" are non-blocking. Explicit source/user origins outrank repository-fact, which outranks model-inference.
 */
export interface DependencyEdge {
  /**
   * Filesystem-safe ULID (Crockford base32, 26 chars).
   */
  id: string;
  orchestration_id: string;
  from_task_id: string;
  to_task_id: string;
  type: "requires" | "produces-input-for" | "serializes-with" | "informational";
  /**
   * Why this edge exists: an explicit source link, a repository fact, or a model-inference rationale.
   */
  evidence?: string;
  origin: "explicit-source" | "user" | "repository-fact" | "model-inference";
  confidence: number;
  status: "active" | "removed";
  /**
   * Required to remove an edge from a task that has already been routed; not required before routing.
   */
  removal_evidence?: string;
  created_at: string;
  /**
   * Optimistic-concurrency counter; incremented on every applied event.
   */
  revision: number;
}
