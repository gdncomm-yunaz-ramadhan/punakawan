/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One reviewer's recorded outcome for a lane's current attempt, computed and stored the same way VerificationMatrix is (a scan of the lane's own event log, not a DeliveryLane field), since a lane may accumulate more than one conclusion across retries. independence_level and the reviewer's own worker/session/model/provider identity are recorded so a later reader can tell whether this conclusion came from a genuinely separate reviewer rather than the same session that implemented the change - recording the same session as reviewer is rejected unless an override reason is given.
 */
export interface ReviewConclusion {
  /**
   * Filesystem-safe ULID (Crockford base32, 26 chars).
   */
  id: string;
  lane_id: string;
  outcome: "approved" | "changes_requested" | "blocked";
  reviewer_worker_id: string;
  reviewer_session_id: string;
  reviewer_model?: string;
  reviewer_provider?: string;
  /**
   * How independent this reviewer was from whoever implemented the attempt being reviewed.
   */
  independence_level: "same_session" | "different_session" | "different_worker";
  /**
   * Required to record a conclusion whose reviewer_session_id matches the implementer's own session; absent otherwise.
   */
  independence_override_reason?: string;
  /**
   * EvidenceArtifact ids the reviewer's conclusion is grounded in.
   */
  evidence_ids: string[];
  /**
   * ReviewFinding ids that must be resolved before this conclusion could become approved; empty when the outcome is already approved.
   */
  blocking_finding_ids: string[];
  /**
   * The computed_at of the VerificationMatrix the reviewer actually looked at - pins exactly what verification state this conclusion was formed against.
   */
  verification_matrix_computed_at: string;
  recorded_at: string;
}
