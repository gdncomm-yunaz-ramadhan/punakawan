/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const GitCapabilitiesSchema = z.object({ "detected": z.boolean(), "repository_root": z.string().optional(), "is_worktree": z.boolean().optional(), "is_bare_repository": z.boolean().optional(), "current_branch": z.string().optional(), "detached_head": z.boolean(), "default_branch": z.string().optional(), "has_uncommitted_changes": z.boolean(), "has_untracked_files": z.boolean(), "remotes": z.array(z.object({ "name": z.string(), "fetch_url": z.string(), "push_url": z.string().optional() }).strict()), "provider": z.enum(["github","gitlab","bitbucket","generic"]).optional(), "capabilities": z.object({ "inspect_history": z.boolean(), "create_branch": z.boolean(), "create_worktree": z.boolean(), "commit": z.boolean(), "push": z.boolean(), "create_pull_request": z.boolean(), "read_pull_request": z.boolean(), "comment_pull_request": z.boolean() }).strict(), "limitations": z.array(z.string()) }).strict().describe("What Punakawan detected about a repository's git/remote/provider state. See punakawan-architecture-enhancement-plan.md §7.2-7.3.")
export type GitCapabilitiesSchema = z.infer<typeof GitCapabilitiesSchema>
