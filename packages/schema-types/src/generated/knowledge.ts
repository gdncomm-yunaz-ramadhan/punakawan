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
    | "final-plan"
    | "retrieval-recipe";
  status: string;
  title: string;
  /**
   * A short human-readable summary, indexed for search alongside title/content. See punakawan-architecture-enhancement-plan.md §10.4/§11.4.
   */
  summary?: string;
  /**
   * The record's full text body, the lowest-weighted BM25F search field. See §11.4/§11.5.
   */
  content?: string;
  /**
   * Alternate names this record is also known by (e.g. an acronym or a renamed component), matched with a strong ranking bonus. See §11.7.
   */
  aliases?: string[];
  /**
   * Free-form labels for filtering and search. See §11.4/§11.5.
   */
  tags?: string[];
  /**
   * Where this record applies, used for search scope boosting. See §10.4/§11.10.
   */
  scope?: {
    organization?: string;
    project?: string;
    repository?: string;
    module?: string;
    path?: string;
    symbol?: string;
  };
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
    /**
     * draft and validating are only meaningful for retrieval-recipe records (punakawan-procedural-knowledge-retrieval-recipe-plan-final.md §12): a recipe starts draft while being taught, moves to validating during its provider dry-run, then verified once explicitly accepted. Every other record type goes straight to observed/inferred/assumed/verified.
     */
    state:
      | "observed"
      | "inferred"
      | "assumed"
      | "verified"
      | "disputed"
      | "superseded"
      | "invalid"
      | "stale"
      | "draft"
      | "validating";
    verified_by?: string[];
    verified_at?: string;
  };
  /**
   * The id of the knowledge record that supersedes this one, set by Store.Supersede without deleting or overwriting this record. See punakawan-architecture-enhancement-plan.md §10.4.
   */
  superseded_by?: string;
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
  /**
   * A declarative, provider-tested procedure for a recurring read-only lookup, present on retrieval-recipe records. See punakawan-procedural-knowledge-retrieval-recipe-plan-final.md §3-4. Lifecycle state lives in validity.state (reusing the shared enum, not duplicated here); this object holds recipe-specific binding, selector, and evidence data only. Immutable versioning is achieved the same way every other knowledge record is corrected: a new id/version is Put and the old one's superseded_by is set, with relations[{type:supersedes,target:<old-id>}] recording the forward link - no separate versioning mechanism is introduced.
   */
  retrieval_recipe?: {
    /**
     * Capability registry identifier this recipe is bound to, e.g. jira.issue.search.
     */
    capability: string;
    /**
     * Typed operation intent this recipe answers, e.g. project.next-sprint.issues.
     */
    intent: string;
    provider: string;
    resource: string;
    operation: string;
    /**
     * Must be true for every recipe this phase's engine can execute (§14: a future write-capable ActionRecipe is a separate type, gated by the approval engine, not a flag here).
     */
    read_only: true;
    /**
     * Monotonically increasing per capability+intent+scope lineage; incremented on every accepted correction.
     */
    recipe_version?: number;
    applies_to?: {
      workspace_ids?: string[];
      repository_ids?: string[];
    };
    inputs?: {
      name: string;
      type: string;
      required?: boolean;
      default?: string;
    }[];
    /**
     * Structured selector AST (§5): a group of clauses. Bounded at two nesting levels - a clause may itself be a nested any/all group, but that group's own clauses are leaves only (field+operator+value, no further any/all). The real Jira next-sprint example never nests deeper than this; a provider needing deeper nesting is a schema revision for a later phase, not a silent extension here. No $ref/$defs is used anywhere in this repo's schemas yet, so the leaf clause shape is duplicated inline (all/any/leaf) rather than risk being the first to rely on untested go-jsonschema $ref support.
     */
    selector: {
      all?: {
        field?: string;
        operator?:
          "equals" | "not_equals" | "phrase_contains" | "contains" | "in" | "not_in" | "greater_than" | "less_than";
        value?:
          | {
              literal: string | number | boolean;
            }
          | {
              resolver: string;
              arguments?: {
                [k: string]: unknown;
              };
            };
        all?: {
          field?: string;
          operator?:
            "equals" | "not_equals" | "phrase_contains" | "contains" | "in" | "not_in" | "greater_than" | "less_than";
          value?:
            | {
                literal: string | number | boolean;
              }
            | {
                resolver: string;
                arguments?: {
                  [k: string]: unknown;
                };
              };
        }[];
        any?: {
          field?: string;
          operator?:
            "equals" | "not_equals" | "phrase_contains" | "contains" | "in" | "not_in" | "greater_than" | "less_than";
          value?:
            | {
                literal: string | number | boolean;
              }
            | {
                resolver: string;
                arguments?: {
                  [k: string]: unknown;
                };
              };
        }[];
      }[];
      any?: {
        field?: string;
        operator?:
          "equals" | "not_equals" | "phrase_contains" | "contains" | "in" | "not_in" | "greater_than" | "less_than";
        value?:
          | {
              literal: string | number | boolean;
            }
          | {
              resolver: string;
              arguments?: {
                [k: string]: unknown;
              };
            };
        all?: {
          field?: string;
          operator?:
            "equals" | "not_equals" | "phrase_contains" | "contains" | "in" | "not_in" | "greater_than" | "less_than";
          value?:
            | {
                literal: string | number | boolean;
              }
            | {
                resolver: string;
                arguments?: {
                  [k: string]: unknown;
                };
              };
        }[];
        any?: {
          field?: string;
          operator?:
            "equals" | "not_equals" | "phrase_contains" | "contains" | "in" | "not_in" | "greater_than" | "less_than";
          value?:
            | {
                literal: string | number | boolean;
              }
            | {
                resolver: string;
                arguments?: {
                  [k: string]: unknown;
                };
              };
        }[];
      }[];
    };
    ordering?: {
      field: string;
      direction: "ascending" | "descending";
    }[];
    output: {
      entity_type: string;
      identity_field: string;
      fields: string[];
    };
    /**
     * The most recent validation/acceptance report (§9, §4's validation: block). Sample results and full compiled-query text are stored as evidence records, referenced by id here, not inlined.
     */
    validation?: {
      status?: "pending" | "passed" | "failed";
      validation_id?: string;
      provider_instance_fingerprint?: string;
      compiled_query_hash?: string;
      sample_size?: number;
      accepted_result_count?: number;
      accepted_by?: string;
      accepted_at?: string;
      evidence_ids?: string[];
    };
    /**
     * The most recent execution's evidence summary (§13). Full history lives in evidence records referenced by evidence_id; this is only the latest, for quick display.
     */
    last_execution?: {
      session_id?: string;
      task_id?: string;
      executed_at?: string;
      bindings?: {
        [k: string]: unknown;
      };
      compiled_query_hash?: string;
      result_count?: number;
      provider_request_id?: string;
      status?: "success" | "failure";
      evidence_id?: string;
    };
  };
}
