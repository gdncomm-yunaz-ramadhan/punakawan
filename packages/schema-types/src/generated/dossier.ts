/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * The primary proof artifact for a change, per punakawan-role-config-distinguished-improvements-plan.md Part IV §32/§33. Machine- and human-readable, versioned, project-scoped, evidence-backed, exportable. It answers what was requested, what evidence defined it, what contradictions existed, what was affected, what plan was accepted, what changed, what was tested, whether implementation matched the plan, and what remains unresolved. Claims and evidence live in sibling records (DossierClaim / DossierEvidence); this manifest links them by id.
 */
export interface ChangeDossier {
  version: "punakawan.change-dossier/v1";
  id: string;
  project_id: string;
  title: string;
  status:
    | "draft"
    | "context_ready"
    | "planned"
    | "implementing"
    | "awaiting_verification"
    | "verified"
    | "disputed"
    | "completed"
    | "superseded";
  created_at?: string;
  updated_at?: string;
  objective: {
    statement: string;
    source_refs?: string[];
  };
  requirements?: {
    covered?: string[];
    uncovered?: string[];
  };
  contradictions?: {
    resolved?: string[];
    unresolved?: string[];
  };
  impact?: {
    repositories?: string[];
    excluded_repositories?: {
      repository: string;
      reason: string;
    }[];
    missing_coverage?: string[];
  };
  plan?: {
    id?: string;
    version?: number;
  };
  tasks?: {
    completed?: string[];
  };
  implementation?: {
    changed_repositories?: string[];
  };
  /**
   * Per-dimension verification status. Values are claim-status strings (draft/claimed/supported/verified/disputed/rejected).
   */
  verification?: {
    [k: string]: string;
  };
  plan_conformance?: {
    implemented?: number;
    partial?: number;
    missing?: number;
    deliberate_deviations?: {
      item: string;
      actual: string;
      rationale: string;
      approved?: boolean;
    }[];
  };
  unresolved_risks?: string[];
  rollback?: {
    verified?: boolean;
    procedure?: string;
  };
  /**
   * DossierClaim ids attached to this dossier.
   */
  claims?: string[];
  /**
   * DossierEvidence ids attached to this dossier.
   */
  evidence?: string[];
}
