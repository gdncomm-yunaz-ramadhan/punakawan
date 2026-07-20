/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * A dependency-aware Beads work item generated from an approved plan. See punakawan-go-typescript-detailed-plan.md §10.
 */
export interface TaskContract {
  id: string;
  requirement_id: string;
  jira_key?: string;
  beads_epic?: string;
  repository: string;
  dependencies?: {
    type: "blocks" | "discovered-from" | "requires";
    id: string;
  }[];
  scope: string;
  expected_files_or_components?: string[];
  /**
   * @minItems 1
   */
  acceptance_criteria: [string, ...string[]];
  test_requirements?: string[];
  required_evidence?: string[];
  risk_classification?: "low" | "medium" | "high";
  approval_required?: boolean;
  definition_of_done: string;
  discovered_from?: string;
}
