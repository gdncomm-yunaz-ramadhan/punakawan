/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One reported status of a single CI check run against a lane's branch (e.g. one GitHub check run, one Jenkins job). A lane's CI dimension in its VerificationMatrix is derived by folding every reported CICheck for that lane, grouped by external_id, keeping the latest status per check - this record is the raw input to that fold, not itself a derived state.
 */
export interface CICheck {
  /**
   * Which CI system reported this check, so the same external_id from two different providers is never confused with the same check.
   */
  provider: "github" | "jenkins" | "generic";
  /**
   * The provider's own stable identifier for this check (e.g. a GitHub check-run id, a Jenkins job/build key). Distinct checks are grouped by this id when folding, not by name, since a check's display name can be reused across unrelated jobs.
   */
  external_id: string;
  /**
   * Human-readable check name, for display only.
   */
  name: string;
  status: "queued" | "running" | "passed" | "failed" | "cancelled";
  /**
   * Whether this check gates the lane's CI dimension. A lane's derived CI status only depends on required checks - an optional check failing never fails the CI dimension.
   */
  required: boolean;
  /**
   * Link to the check's own detail page, if the provider exposes one.
   */
  url?: string;
  reported_at: string;
}
