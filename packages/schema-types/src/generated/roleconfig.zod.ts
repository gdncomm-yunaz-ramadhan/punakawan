/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const RolePreferencesSchema = z.object({ "version": z.literal(2), "revision": z.number().int().gte(0), "roles": z.object({ "semar": z.any(), "gareng": z.any(), "petruk": z.any(), "bagong": z.any() }).strict() }).strict().describe("Per-project prompt preferences for the four Punakawan roles (Semar coordinates, Gareng challenges, Petruk plans and builds, Bagong verifies). Persisted at .punakawan/roles.yaml. Style selects one of three fixed prompt-guidance strings appended to a role's prompt; instructions is free text appended after that guidance. This configuration only shapes prompt wording: it never authorizes tools, grants permissions, or changes what a workflow requires. revision is bumped on every save for optimistic locking.")
export type RolePreferencesSchema = z.infer<typeof RolePreferencesSchema>
