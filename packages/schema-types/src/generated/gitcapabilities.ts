/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * What Punakawan detected about a repository's git/remote/provider state. See punakawan-architecture-enhancement-plan.md §7.2-7.3.
 */
export interface GitCapabilities {
  detected: boolean;
  repository_root?: string;
  is_worktree?: boolean;
  is_bare_repository?: boolean;
  current_branch?: string;
  detached_head: boolean;
  default_branch?: string;
  has_uncommitted_changes: boolean;
  has_untracked_files: boolean;
  remotes: {
    name: string;
    fetch_url: string;
    push_url?: string;
  }[];
  provider?: "github" | "gitlab" | "bitbucket" | "generic";
  capabilities: {
    inspect_history: boolean;
    create_branch: boolean;
    create_worktree: boolean;
    commit: boolean;
    push: boolean;
    create_pull_request: boolean;
    read_pull_request: boolean;
    comment_pull_request: boolean;
  };
  limitations: string[];
}
