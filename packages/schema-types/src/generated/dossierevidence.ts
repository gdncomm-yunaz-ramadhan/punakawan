/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One evidence record within a Change Dossier, per punakawan-role-config-distinguished-improvements-plan.md §35. Records how a claim is backed: the command run (and where), its result, and any produced artifacts with content hashes. Distinct from internal task Evidence; this is the dossier's exportable, hash-anchored proof unit.
 */
export interface DossierEvidence {
  id: string;
  dossier_id?: string;
  type:
    | "requirement_source"
    | "source_location"
    | "diff"
    | "test_result"
    | "build_result"
    | "api_compatibility"
    | "security_scan"
    | "dependency_scan"
    | "migration_check"
    | "screenshot"
    | "manual_confirmation"
    | "review_result";
  source?: {
    command?: string;
    repository?: string;
    working_tree?: string;
    ref?: string;
  };
  result?: {
    status?: "passed" | "failed" | "unknown";
    exit_code?: number;
  };
  artifacts?: {
    path: string;
    sha256?: string;
  }[];
}
