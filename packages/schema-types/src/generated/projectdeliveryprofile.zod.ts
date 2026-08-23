/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const ProjectDeliveryProfileSchema = z.object({ "id": z.string().describe("Filesystem-safe ULID (Crockford base32, 26 chars)."), "project_id": z.string(), "local_path": z.string().describe("Absolute path to an existing local checkout, if one is already known; absent when only the remote is known and a worktree has not been created yet.").optional(), "canonical_remote": z.string().describe("Remote name (e.g. \"origin\") preflight and worktree creation fetch from.").optional(), "base_branch": z.string(), "provider": z.enum(["github","gitlab","bitbucket","generic"]).optional(), "build_command": z.string().optional(), "test_command": z.string().optional(), "required_executables": z.array(z.string()).optional(), "required_services": z.array(z.string()).describe("Names of external services (referenced by capability status only, per this task's boundary - never a credential value).").optional(), "quality_rules": z.array(z.string()).optional(), "ci_adapter": z.string().optional(), "verification_gates": z.array(z.string()).optional(), "max_concurrent_workers": z.number().int().gte(1).optional(), "revision": z.number().int().gte(0).describe("Optimistic-concurrency counter; incremented on every update.") }).strict().describe("Versioned, per-project delivery configuration: local path, canonical remote, branch policy, build/test commands, required services, and CI/worker policy. Explicit repository configuration takes precedence over global detected or learned defaults; this record only stores the merged, effective values a preflight run and approval manifest are computed against.")
export type ProjectDeliveryProfileSchema = z.infer<typeof ProjectDeliveryProfileSchema>
