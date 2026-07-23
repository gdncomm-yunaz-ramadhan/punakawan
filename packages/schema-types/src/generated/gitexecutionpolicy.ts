/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * Effective git-behavior policy after merging detected GitCapabilities with repository policy and user overrides: 'effective behavior = detected capabilities ∩ repository policy ∩ user permission'. See punakawan-architecture-enhancement-plan.md §7.4-7.5.
 */
export interface GitExecutionPolicy {
  source: "user" | "repository-policy" | "default";
  skip_git: boolean;
  allow_branch_creation: boolean;
  allow_worktree_creation: boolean;
  allow_commit: boolean;
  allow_push: boolean;
  allow_pull_request_creation: boolean;
  reason?: string;
}
