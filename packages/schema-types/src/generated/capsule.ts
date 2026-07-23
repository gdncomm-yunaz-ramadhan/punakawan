/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * An immutable, hashable, bounded context handed to one Gareng/Petruk/Bagong invocation. See punakawan-architecture-enhancement-plan.md §6 and AEP-M1 (punokawan-0m9). Digest is computed over role/objective/requirements/acceptance_criteria/constraints/relevant_knowledge/evidence/allowed_tools/forbidden_actions per §6.3 and must be recomputed (not mutated in place) if any of those fields change.
 */
export interface ContextCapsule {
  id: string;
  digest: string;
  task_id: string;
  created_at: string;
  role: "gareng" | "petruk" | "bagong";
  objective: string;
  requirements?: {
    id: string;
    summary?: string;
  }[];
  acceptance_criteria?: string[];
  constraints?: string[];
  relevant_knowledge?: {
    id: string;
    summary?: string;
    /**
     * Why this item was selected for this capsule, e.g. a search_knowledge match explanation. Set only when the item came from Semar's automatic knowledge-retrieval pipeline (AEP-M7); manually-cited items may omit it. See punakawan-architecture-enhancement-plan.md §11.13.
     */
    reason?: string;
  }[];
  evidence?: {
    id: string;
    summary?: string;
  }[];
  assumptions?: string[];
  unresolved_questions?: string[];
  allowed_tools: string[];
  forbidden_actions: string[];
  expected_output?: string;
  token_budget?: number;
}
