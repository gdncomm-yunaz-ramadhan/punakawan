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
  /**
   * Static repository configuration facts (package manager, layout, naming convention, test framework, linters, formatters). Captured automatically when an agent's tool call touches a recognized config file (go.mod, package.json, a lockfile, .golangci.yml, etc.), or set explicitly via upsert_project. Each field is merged independently on write, never wholesale-replaced, so one detector's update never erases another's.
   */
  metadata?: {
    package_manager?: string;
    layout?: string;
    naming_convention?: string;
    test_framework?: string[];
    linters?: string[];
    formatters?: string[];
    editorconfig?: boolean;
  };
}
