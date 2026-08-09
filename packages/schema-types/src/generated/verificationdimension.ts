/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * The latest known status of one of the fixed dimensions a lane's attempt is verified against before a review conclusion may rely on it. logic/unit/integration/quality/e2e are recorded directly by whichever role or tool ran that check; ci is either recorded directly or derived from folded CICheck reports for the lane, with an explicit recording always taking precedence over the derived value.
 */
export interface VerificationDimension {
  name: "logic" | "unit" | "integration" | "quality" | "e2e" | "ci";
  /**
   * pending means no evidence has been recorded for this dimension yet - it is never defaulted to passed or failed without evidence.
   */
  status: "pending" | "passed" | "failed";
  /**
   * Id of the EvidenceArtifact backing this dimension's status, if the recording included one.
   */
  evidence_id?: string;
  /**
   * Short human-readable explanation of the status, if the recording included one.
   */
  summary?: string;
  /**
   * When this dimension's status was last recorded or derived.
   */
  checked_at?: string;
}
