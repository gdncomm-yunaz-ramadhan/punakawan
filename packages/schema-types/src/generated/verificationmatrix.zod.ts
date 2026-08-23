/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const VerificationMatrixSchema = z.object({ "lane_id": z.string(), "orchestration_id": z.string(), "dimensions": z.array(z.object({ "name": z.enum(["logic","unit","integration","quality","e2e","ci"]), "status": z.enum(["pending","passed","failed"]), "evidence_id": z.string().optional(), "summary": z.string().optional(), "checked_at": z.string().datetime({ offset: true }).optional() }).strict()).describe("Exactly one entry per fixed dimension name (logic, unit, integration, quality, e2e, ci), in that order."), "computed_at": z.string().datetime({ offset: true }).describe("Timestamp of the last lane event folded into this matrix, or the lane's own created_at if it has no events yet - never wall-clock time, so the same event log always computes the same matrix.") }).strict().describe("A computed, non-reduced snapshot of one lane's verification state across its fixed set of dimensions, built by scanning the lane's own event log rather than by adding accumulating fields to DeliveryLane - unlike a lane's single-latest-pointer role-stage fields, a verification matrix is naturally multi-valued and grows over a lane's lifetime as more dimensions are checked. Always carries exactly one entry per fixed dimension name, defaulting to pending when nothing has been recorded for it yet, so a caller never has to special-case a missing dimension.")
export type VerificationMatrixSchema = z.infer<typeof VerificationMatrixSchema>
