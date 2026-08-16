/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One immutable invocation record: a test, diff, API check, command, screenshot, or review produced exactly this content-addressed blob. Bytes are addressed by sha256 (never overwritten, never trusted from a caller - always server-computed); this record's own id is a ULID and is what gets referenced elsewhere, never a mutable path.
 */
export interface EvidenceArtifact {
  /**
   * Filesystem-safe ULID (Crockford base32, 26 chars).
   */
  id: string;
  orchestration_id: string;
  project_id: string;
  lane_id?: string;
  parent_task_id?: string;
  kind: "test" | "diff" | "api-check" | "command" | "screenshot" | "review" | "quality";
  content_hash: string;
  media_type: string;
  byte_size: number;
  /**
   * What invoked this (e.g. "go test", "petruk-plan"), for provenance.
   */
  producer?: string;
  created_at: string;
  /**
   * Retention horizon; absent means retained indefinitely.
   */
  retain_until?: string;
}
