/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * A computed, non-reduced snapshot of one lane's verification state across its fixed set of dimensions, built by scanning the lane's own event log rather than by adding accumulating fields to DeliveryLane - unlike a lane's single-latest-pointer role-stage fields, a verification matrix is naturally multi-valued and grows over a lane's lifetime as more dimensions are checked. Always carries exactly one entry per fixed dimension name, defaulting to pending when nothing has been recorded for it yet, so a caller never has to special-case a missing dimension.
 */
export interface VerificationMatrix {
  lane_id: string;
  orchestration_id: string;
  /**
   * Exactly one entry per fixed dimension name (logic, unit, integration, quality, e2e, ci), in that order.
   */
  dimensions: {
    name: "logic" | "unit" | "integration" | "quality" | "e2e" | "ci";
    status: "pending" | "passed" | "failed";
    evidence_id?: string;
    summary?: string;
    checked_at?: string;
  }[];
  /**
   * Timestamp of the last lane event folded into this matrix, or the lane's own created_at if it has no events yet - never wall-clock time, so the same event log always computes the same matrix.
   */
  computed_at: string;
}
