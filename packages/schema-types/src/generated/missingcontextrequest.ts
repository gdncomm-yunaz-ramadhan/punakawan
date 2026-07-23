/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * A subagent's request for context its capsule did not include, routed back to Semar per punakawan-architecture-enhancement-plan.md §6.4. Punakawan never decides how to resolve it (ADR-0016) - it only persists the request and whichever resolution the calling agent, acting as Semar, chooses.
 */
export interface MissingContextRequest {
  id: string;
  capsule_id: string;
  query: string;
  reason: string;
  preferred_types?: string[];
  blocking: boolean;
  /**
   * pending until Semar resolves it. §6.4's four resolutions: search for the context (not itself a terminal status - that's just a search_knowledge call, which then leads to one of the below), add it to a new capsule revision, reject it as irrelevant, or ask the user.
   */
  status: "pending" | "added_to_revision" | "rejected" | "asked_user";
  resolution_note?: string;
  /**
   * Set when status is added_to_revision: the id of the new ContextCapsule (from request_capsule) that supersedes capsule_id with the missing context included.
   */
  revised_capsule_id?: string;
  created_at: string;
  resolved_at?: string;
}
