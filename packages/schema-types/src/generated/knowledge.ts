/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * A durable knowledge record with provenance and validity state. See punakawan-go-typescript-detailed-plan.md §7.
 */
export interface KnowledgeRecord {
  id: string;
  type:
    | "workspace"
    | "repository"
    | "component"
    | "source-artifact"
    | "requirement"
    | "acceptance-criterion"
    | "claim"
    | "assumption"
    | "constraint"
    | "decision"
    | "question"
    | "answer"
    | "api-contract"
    | "data-contract"
    | "browser-flow"
    | "test-case"
    | "deployment-unit"
    | "work-item"
    | "change-set"
    | "evidence"
    | "external-reference"
    | "person-or-team"
    | "risk"
    | "review-finding";
  status: string;
  title: string;
  source: {
    provider: string;
    external_id?: string;
    version?: string | number;
    uri?: string;
    section?: string;
    content_hash?: string;
    retrieved_at: string;
  };
  extraction: {
    method: "model-assisted" | "manual" | "imported";
    extractor_version?: string;
    confidence?: number;
  };
  validity: {
    state: "observed" | "inferred" | "assumed" | "verified" | "disputed" | "superseded" | "invalid" | "stale";
    verified_by?: string[];
    verified_at?: string;
  };
  relations?: {
    type:
      | "derived-from"
      | "supersedes"
      | "implements"
      | "validates"
      | "tested-by"
      | "automated-by"
      | "deployed-by"
      | "depends-on"
      | "blocks"
      | "related-to"
      | "conflicts-with"
      | "owned-by"
      | "applies-to"
      | "verified-by"
      | "discovered-from"
      | "documented-in"
      | "tracked-by"
      | "observed-in"
      | "assumes"
      | "resolves";
    target: string;
  }[];
}
