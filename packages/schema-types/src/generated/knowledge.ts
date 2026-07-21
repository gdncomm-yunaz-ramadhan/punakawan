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
    | "review-finding"
    | "convention-profile"
    | "context-dossier"
    | "semar-synthesis"
    | "gareng-review"
    | "petruk-plan"
    | "bagong-review"
    | "final-plan";
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
  /**
   * Formatting conventions, present on convention-profile records. See §27.3.
   */
  formatting?: {
    editorconfig?: boolean;
    linters?: string[];
    formatters?: string[];
  };
  /**
   * Structural conventions, present on convention-profile records. See §27.3.
   */
  structure?: {
    layout?: string;
    package_manager?: string;
    test_framework?: string[];
    naming_convention?: string;
  };
  /**
   * Semar's pre-planning context dossier, present on context-dossier records. See §9.1.
   */
  context_dossier?: {
    user_goal?: string;
    business_or_user_value?: string;
    current_behavior?: string;
    desired_behavior?: string;
    explicit_non_goals?: string[];
    source_inventory?: string[];
    affected_repositories?: string[];
    existing_implementation_paths?: string[];
    existing_tests?: string[];
    api_and_data_contracts?: string[];
    deployment_path?: string;
    relevant_previous_decisions?: string[];
    assumptions?: string[];
    missing_information?: string[];
    contradictions?: string[];
    confidence_level?: string;
  };
  /**
   * Semar's consolidated output after merging Gareng and Petruk findings, present on semar-synthesis records. See §8.1, §9.2.
   */
  semar_synthesis?: {
    goal?: string;
    scope?: string;
    known_facts?: string[];
    assumptions?: string[];
    open_questions?: {
      question?: string;
      why_it_matters?: string;
      observed_conflict?: string;
      recommended_default?: string;
      impact_if_unanswered?: string;
      blocking?: boolean;
      target?: {
        system?: string;
        reference?: string;
      };
    }[];
    affected_repositories?: string[];
    affected_components?: string[];
    risks?: string[];
    recommended_workflow?: string;
    next_gate?: string;
  };
  /**
   * Gareng's feasibility and risk review, present on gareng-review records. See §8.2.
   */
  gareng_review?: {
    verdict?: string;
    blocking_findings?: string[];
    non_blocking_findings?: string[];
    missing_acceptance_criteria?: string[];
    risks?: string[];
    recommended_defaults?: string[];
    required_evidence?: string[];
  };
  /**
   * Petruk's usefulness challenge and implementation planning output, present on petruk-plan records. See §8.3.
   */
  petruk_plan?: {
    recommended_solution?: string;
    alternatives?: string[];
    tradeoffs?: string[];
    implementation_steps?: string[];
    repository_changes?: string[];
    test_plan?: string[];
    e2e_plan?: string[];
    deployment_plan?: string[];
    documentation_plan?: string[];
  };
  /**
   * Bagong's independent final review, present on bagong-review records. See §8.4.
   */
  bagong_review?: {
    verdict?: string;
    requirement_coverage?: string[];
    findings?: string[];
    blocking_findings?: string[];
    test_gaps?: string[];
    security_findings?: string[];
    compatibility_findings?: string[];
    uncertainties?: string[];
    honest_summary?: string;
  };
  /**
   * Semar's final implementation plan, present on final-plan records. See §9.3.
   */
  final_plan?: {
    requirements?: string[];
    acceptance_criteria?: string[];
    non_goals?: string[];
    architecture_decision?: string;
    data_model_impact?: string;
    api_impact?: string;
    repository_impact_map?: {
      [k: string]: string;
    };
    implementation_sequence?: string[];
    unit_test_plan?: string[];
    integration_test_plan?: string[];
    e2e_plan?: string[];
    migration_plan?: string[];
    rollback_plan?: string[];
    observability_plan?: string[];
    documentation_plan?: string[];
    deployment_changes?: string[];
    security_considerations?: string[];
    compatibility_considerations?: string[];
    verification_criteria?: string[];
    risks_and_mitigations?: string[];
  };
}
