/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * An immutable, canonicalized snapshot of one requirement input (Jira, Confluence, GitHub, a document URL, or free text) captured into a DeliveryOrchestration (punokawan-14yn.2). canonical_key is an exact, provider-specific identifier (never a fuzzy/similar-wording match), so a pinned requirement can never be silently replaced by a similar retrieved result. Grouped into a ParentTask once routing/decomposition decides which task it belongs to. See affiliate-platform-delivery-feedback-2026-08-07.md.
 */
export interface RequirementSource {
  /**
   * Filesystem-safe ULID (Crockford base32, 26 chars).
   */
  id: string;
  orchestration_id: string;
  provider: "jira" | "confluence" | "github" | "url" | "freetext";
  /**
   * The provider's own identifier (issue key, page id, issue/PR number); absent for freetext.
   */
  external_id?: string;
  /**
   * Exact dedup/pin key, e.g. "jira:PAY-1842" or "url:https://example.com/doc". Never derived from fuzzy text similarity.
   */
  canonical_key: string;
  content_hash: string;
  title: string;
  summary?: string;
  /**
   * Set when this source is a Jira/GitHub subtask resolved to its source parent; the referenced RequirementSource must already exist in the same orchestration.
   */
  parent_source_id?: string;
  captured_at: string;
  /**
   * Optimistic-concurrency counter; incremented on every applied event.
   */
  revision: number;
}
