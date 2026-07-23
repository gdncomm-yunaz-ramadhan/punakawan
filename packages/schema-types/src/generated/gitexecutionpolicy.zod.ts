/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const GitExecutionPolicySchema = z.object({ "source": z.enum(["user","repository-policy","default"]), "skip_git": z.boolean(), "allow_branch_creation": z.boolean(), "allow_worktree_creation": z.boolean(), "allow_commit": z.boolean(), "allow_push": z.boolean(), "allow_pull_request_creation": z.boolean(), "reason": z.string().optional() }).strict().describe("Effective git-behavior policy after merging detected GitCapabilities with repository policy and user overrides: 'effective behavior = detected capabilities ∩ repository policy ∩ user permission'. See punakawan-architecture-enhancement-plan.md §7.4-7.5.")
export type GitExecutionPolicySchema = z.infer<typeof GitExecutionPolicySchema>
