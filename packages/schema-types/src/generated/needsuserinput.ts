/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * A direct, non-persisted clarification result returned instead of a mutation when required context is missing/contradictory or a material decision has more than one defensible answer. See the improvement plan's Delivery contract and Public result semantics.
 */
export interface NeedUserInput {
  kind: "missing_context" | "decision_required";
  question: string;
  missing_fields?: string[];
  options?: {
    id: string;
    label: string;
    impact: string;
  }[];
}
