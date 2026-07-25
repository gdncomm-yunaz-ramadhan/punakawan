/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * A durable record of a disagreement between sources (Jira, Confluence, project metadata, workflows, knowledge, plans, source code, configuration, tests, API specs), per punakawan-role-config-distinguished-improvements-plan.md Part II. Punakawan must not silently choose between conflicting sources: every material disagreement becomes one of these records, carrying each side's claim + evidence and an optional proposed resolution that requires human confirmation.
 */
export interface Contradiction {
  version: "punakawan.contradiction/v1";
  id: string;
  project_id: string;
  title: string;
  /**
   * informational: record only. minor: warn. material: clarify or resolve. critical: block.
   */
  severity: "informational" | "minor" | "material" | "critical";
  status:
    | "detected"
    | "triaged"
    | "needs_clarification"
    | "resolution_proposed"
    | "resolved"
    | "accepted_divergence"
    | "superseded";
  /**
   * Whether this contradiction currently blocks progress. Only Gareng's blocking_risks capability (or project policy for critical) may set it.
   */
  blocking?: boolean;
  created_at?: string;
  updated_at?: string;
  /**
   * Role or subsystem that detected the contradiction.
   */
  detected_by?: string;
  subject: {
    type:
      | "configuration"
      | "api_operation"
      | "requirement"
      | "knowledge"
      | "plan"
      | "source"
      | "test"
      | "metadata"
      | "other";
    /**
     * Normalized identifier for the thing in disagreement (config key, operation id, requirement id, ...).
     */
    key?: string;
  };
  /**
   * @minItems 2
   */
  claims: [
    {
      source: {
        type: "confluence" | "jira" | "repository" | "metadata" | "knowledge" | "plan" | "test" | "openapi" | "other";
        /**
         * Reference within the source (page id, issue key, file path, ...).
         */
        ref?: string;
      };
      statement: string;
      /**
       * Evidence record ids supporting this claim.
       */
      evidence?: string[];
    },
    {
      source: {
        type: "confluence" | "jira" | "repository" | "metadata" | "knowledge" | "plan" | "test" | "openapi" | "other";
        /**
         * Reference within the source (page id, issue key, file path, ...).
         */
        ref?: string;
      };
      statement: string;
      /**
       * Evidence record ids supporting this claim.
       */
      evidence?: string[];
    },
    ...{
      source: {
        type: "confluence" | "jira" | "repository" | "metadata" | "knowledge" | "plan" | "test" | "openapi" | "other";
        /**
         * Reference within the source (page id, issue key, file path, ...).
         */
        ref?: string;
      };
      statement: string;
      /**
       * Evidence record ids supporting this claim.
       */
      evidence?: string[];
    }[]
  ];
  resolution?: {
    proposed_statement?: string;
    rationale?: string;
    requires_human_confirmation?: boolean;
    /**
     * The confirmed statement once resolved.
     */
    resolved_statement?: string;
    resolved_by?: string;
    resolved_at?: string;
  };
  /**
   * Ids of related entities this contradiction affects (§22 detail 'affected ...').
   */
  links?: {
    plans?: string[];
    tasks?: string[];
    dossiers?: string[];
    handoffs?: string[];
    repositories?: string[];
  };
}
