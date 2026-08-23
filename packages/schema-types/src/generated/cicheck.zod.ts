/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const CICheckSchema = z.object({ "provider": z.enum(["github","jenkins","generic"]).describe("Which CI system reported this check, so the same external_id from two different providers is never confused with the same check."), "external_id": z.string().describe("The provider's own stable identifier for this check (e.g. a GitHub check-run id, a Jenkins job/build key). Distinct checks are grouped by this id when folding, not by name, since a check's display name can be reused across unrelated jobs."), "name": z.string().describe("Human-readable check name, for display only."), "status": z.enum(["queued","running","passed","failed","cancelled"]), "required": z.boolean().describe("Whether this check gates the lane's CI dimension. A lane's derived CI status only depends on required checks - an optional check failing never fails the CI dimension."), "url": z.string().describe("Link to the check's own detail page, if the provider exposes one.").optional(), "reported_at": z.string().datetime({ offset: true }) }).strict().describe("One reported status of a single CI check run against a lane's branch (e.g. one GitHub check run, one Jenkins job). A lane's CI dimension in its VerificationMatrix is derived by folding every reported CICheck for that lane, grouped by external_id, keeping the latest status per check - this record is the raw input to that fold, not itself a derived state.")
export type CICheckSchema = z.infer<typeof CICheckSchema>
