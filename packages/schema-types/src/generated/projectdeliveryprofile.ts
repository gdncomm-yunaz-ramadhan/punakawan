/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * Versioned, per-project delivery configuration: local path, canonical remote, branch policy, build/test commands, required services, and CI/worker policy. Explicit repository configuration takes precedence over global detected or learned defaults; this record only stores the merged, effective values a preflight run and approval manifest are computed against.
 */
export interface ProjectDeliveryProfile {
  /**
   * Filesystem-safe ULID (Crockford base32, 26 chars).
   */
  id: string;
  project_id: string;
  /**
   * Absolute path to an existing local checkout, if one is already known; absent when only the remote is known and a worktree has not been created yet.
   */
  local_path?: string;
  /**
   * Remote name (e.g. "origin") preflight and worktree creation fetch from.
   */
  canonical_remote?: string;
  base_branch: string;
  provider?: "github" | "gitlab" | "bitbucket" | "generic";
  build_command?: string;
  test_command?: string;
  required_executables?: string[];
  /**
   * Names of external services (referenced by capability status only, per this task's boundary - never a credential value).
   */
  required_services?: string[];
  quality_rules?: string[];
  ci_adapter?: string;
  verification_gates?: string[];
  max_concurrent_workers?: number;
  /**
   * Optimistic-concurrency counter; incremented on every update.
   */
  revision: number;
}
