/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const RoleConfigurationSchema = z.object({ "version": z.literal("punakawan.roles/v1"), "revision": z.number().int().gte(0), "roles": z.object({ "semar": z.any(), "gareng": z.any(), "petruk": z.any(), "bagong": z.any() }).strict() }).strict().describe("Per-project configuration for the four Punakawan roles (Semar coordinates, Gareng challenges, Petruk plans and builds, Bagong verifies). Persisted at .punakawan/roles.yaml. See punakawan-role-config-distinguished-improvements-plan.md Part I. User-facing surface is deliberately small: enabled, style, mode, and a short list of role-specific capability toggles. Style changes reasoning behavior, not permissions; Mode gates whether a role may read (assist), propose (propose), or execute (execute) durable changes, always still constrained by workflow restrictions and project policy. revision is bumped on every save for optimistic locking.")
export type RoleConfigurationSchema = z.infer<typeof RoleConfigurationSchema>
