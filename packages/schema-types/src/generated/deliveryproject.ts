/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * A registered project binding in the canonical multi-project delivery control plane. One row per project the orchestrator is allowed to route work to; unknown project ids are never inferred from ambient working-directory scope.
 */
export interface DeliveryProject {
  /**
   * Filesystem-safe ULID (Crockford base32, 26 chars).
   */
  id: string;
  /**
   * Short, filesystem-safe, human-chosen project identifier used in worktree and lane paths.
   */
  slug: string;
  repository_url: string;
  default_branch?: string;
  status: "active" | "disabled";
  registered_at: string;
  /**
   * Optimistic-concurrency counter; incremented on every update.
   */
  revision: number;
}
