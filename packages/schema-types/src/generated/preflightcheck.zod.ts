/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const PreflightCheckSchema = z.object({ "name": z.string(), "status": z.enum(["pass","fail","skipped"]), "classification": z.enum(["required","optional","delegated-to-ci"]), "detail": z.string().optional() }).strict().describe("One capability check result computed for a ProjectDeliveryProfile before code mutation is allowed (punokawan-14yn.4). status=skipped means the check was not actually evaluated (e.g. no adapter yet implements it) - it is never reported as passed without being run. See affiliate-platform-delivery-feedback-2026-08-07.md.")
export type PreflightCheckSchema = z.infer<typeof PreflightCheckSchema>
