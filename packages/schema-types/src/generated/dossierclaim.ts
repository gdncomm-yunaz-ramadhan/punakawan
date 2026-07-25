/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One claim within a Change Dossier, per punakawan-role-config-distinguished-improvements-plan.md §34. A claim has a producer role and a status in the evidence ladder; a role can never verify its own claim (enforced by the store): Petruk produces implementation claims, Gareng risk/feasibility, Semar completeness/coordination, and Bagong verifies or disputes.
 */
export interface DossierClaim {
  id: string;
  dossier_id?: string;
  /**
   * e.g. compatibility, implementation, risk, completeness.
   */
  type: string;
  statement: string;
  producer: {
    role: "semar" | "gareng" | "petruk" | "bagong";
  };
  /**
   * The §2.3 evidence ladder.
   */
  status: "draft" | "claimed" | "supported" | "verified" | "disputed" | "rejected" | "superseded";
  /**
   * DossierEvidence ids supporting the claim.
   */
  evidence?: string[];
  verification?: {
    role?: "semar" | "gareng" | "petruk" | "bagong";
    result?: "verified" | "disputed";
    note?: string;
    at?: string;
  };
}
