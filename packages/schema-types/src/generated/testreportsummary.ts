/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * A bounded, causal-first projection of one test invocation's output (punokawan-14yn.7), concise enough for a continuous agent loop while the full stdout/stderr remains available, unmodified, as the referenced EvidenceArtifact. Never returned instead of the full log - only alongside it.
 */
export interface TestReportSummary {
  command: string;
  exit_code: number;
  duration_ms: number;
  total_tests?: number;
  passed?: number;
  failed?: number;
  skipped?: number;
  /**
   * The deepest "Caused by:" line, or the first recognized failure signature, extracted from the combined output.
   */
  first_causal_failure?: string;
  /**
   * Bounded, retry-noise-deduplicated tail of combined stdout+stderr.
   */
  tail: string;
  truncated: boolean;
  /**
   * EvidenceArtifact id of the full, untruncated combined log.
   */
  artifact_id: string;
}
