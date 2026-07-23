/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One deduplicated, prioritized finding from review_pr's pipeline: Gareng/Petruk build independent review capsules, Bagong verifies findings against the diff and evidence, Semar deduplicates and prioritizes. See punakawan-architecture-enhancement-plan.md §8.2.
 */
export interface ReviewFinding {
  id: string;
  severity: "blocker" | "major" | "minor" | "suggestion";
  category: string;
  title: string;
  explanation: string;
  file?: string;
  start_line?: number;
  end_line?: number;
  evidence: {
    id: string;
    summary?: string;
  }[];
  related_knowledge: {
    id: string;
    summary?: string;
  }[];
  suggested_fix?: string;
  confidence: number;
}
