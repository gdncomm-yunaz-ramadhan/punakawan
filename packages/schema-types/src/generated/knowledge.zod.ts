/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const KnowledgeRecordSchema = z.object({ "id": z.string().regex(new RegExp("^pkw:[a-z]+/[a-z0-9-]+/.+$")), "type": z.enum(["workspace","repository","component","source-artifact","requirement","acceptance-criterion","claim","assumption","constraint","decision","question","answer","api-contract","data-contract","browser-flow","test-case","deployment-unit","work-item","change-set","evidence","external-reference","person-or-team","risk","review-finding","convention-profile","context-dossier","semar-synthesis","gareng-review","petruk-plan","bagong-review","final-plan","retrieval-recipe"]), "status": z.string(), "title": z.string(), "summary": z.string().describe("A short human-readable summary, indexed for search alongside title/content. See punakawan-architecture-enhancement-plan.md §10.4/§11.4.").optional(), "content": z.string().describe("The record's full text body, the lowest-weighted BM25F search field. See §11.4/§11.5.").optional(), "aliases": z.array(z.string()).describe("Alternate names this record is also known by (e.g. an acronym or a renamed component), matched with a strong ranking bonus. See §11.7.").optional(), "tags": z.array(z.string()).describe("Free-form labels for filtering and search. See §11.4/§11.5.").optional(), "scope": z.object({ "organization": z.string().optional(), "project": z.string().optional(), "repository": z.string().optional(), "module": z.string().optional(), "path": z.string().optional(), "symbol": z.string().optional() }).strict().describe("Where this record applies, used for search scope boosting. See §10.4/§11.10.").optional(), "source": z.object({ "provider": z.string(), "external_id": z.string().optional(), "version": z.union([z.string(), z.number().int()]).optional(), "uri": z.string().optional(), "section": z.string().optional(), "content_hash": z.string().regex(new RegExp("^sha256:[0-9a-f]{64}$")).optional(), "retrieved_at": z.string().datetime({ offset: true }) }).strict(), "extraction": z.object({ "method": z.enum(["model-assisted","manual","imported"]), "extractor_version": z.string().optional(), "confidence": z.number().gte(0).lte(1).optional() }).strict(), "validity": z.object({ "state": z.enum(["observed","inferred","assumed","verified","disputed","superseded","invalid","stale","draft","validating"]).describe("draft and validating are only meaningful for retrieval-recipe records (punakawan-procedural-knowledge-retrieval-recipe-plan-final.md §12): a recipe starts draft while being taught, moves to validating during its provider dry-run, then verified once explicitly accepted. Every other record type goes straight to observed/inferred/assumed/verified."), "verified_by": z.array(z.string()).optional(), "verified_at": z.string().datetime({ offset: true }).optional() }).strict(), "superseded_by": z.string().describe("The id of the knowledge record that supersedes this one, set by Store.Supersede without deleting or overwriting this record. See punakawan-architecture-enhancement-plan.md §10.4.").optional(), "relations": z.array(z.object({ "type": z.enum(["derived-from","supersedes","implements","validates","tested-by","automated-by","deployed-by","depends-on","blocks","related-to","conflicts-with","owned-by","applies-to","verified-by","discovered-from","documented-in","tracked-by","observed-in","assumes","resolves"]), "target": z.string() }).strict()).optional(), "formatting": z.object({ "editorconfig": z.boolean().optional(), "linters": z.array(z.string()).optional(), "formatters": z.array(z.string()).optional() }).strict().describe("Formatting conventions, present on convention-profile records. See §27.3.").optional(), "structure": z.object({ "layout": z.string().optional(), "package_manager": z.string().optional(), "test_framework": z.array(z.string()).optional(), "naming_convention": z.string().optional() }).strict().describe("Structural conventions, present on convention-profile records. See §27.3.").optional(), "context_dossier": z.object({ "user_goal": z.string().optional(), "business_or_user_value": z.string().optional(), "current_behavior": z.string().optional(), "desired_behavior": z.string().optional(), "explicit_non_goals": z.array(z.string()).optional(), "source_inventory": z.array(z.string()).optional(), "affected_repositories": z.array(z.string()).optional(), "existing_implementation_paths": z.array(z.string()).optional(), "existing_tests": z.array(z.string()).optional(), "api_and_data_contracts": z.array(z.string()).optional(), "deployment_path": z.string().optional(), "relevant_previous_decisions": z.array(z.string()).optional(), "assumptions": z.array(z.string()).optional(), "missing_information": z.array(z.string()).optional(), "contradictions": z.array(z.string()).optional(), "confidence_level": z.string().optional() }).strict().describe("Semar's pre-planning context dossier, present on context-dossier records. See §9.1.").optional(), "semar_synthesis": z.object({ "goal": z.string().optional(), "scope": z.string().optional(), "known_facts": z.array(z.string()).optional(), "assumptions": z.array(z.string()).optional(), "open_questions": z.array(z.object({ "question": z.string().optional(), "why_it_matters": z.string().optional(), "observed_conflict": z.string().optional(), "recommended_default": z.string().optional(), "impact_if_unanswered": z.string().optional(), "blocking": z.boolean().optional(), "target": z.object({ "system": z.string().optional(), "reference": z.string().optional() }).strict().optional() }).strict()).optional(), "affected_repositories": z.array(z.string()).optional(), "affected_components": z.array(z.string()).optional(), "risks": z.array(z.string()).optional(), "recommended_workflow": z.string().optional(), "next_gate": z.string().optional() }).strict().describe("Semar's consolidated output after merging Gareng and Petruk findings, present on semar-synthesis records. See §8.1, §9.2.").optional(), "gareng_review": z.object({ "verdict": z.string().optional(), "blocking_findings": z.array(z.string()).optional(), "non_blocking_findings": z.array(z.string()).optional(), "missing_acceptance_criteria": z.array(z.string()).optional(), "risks": z.array(z.string()).optional(), "recommended_defaults": z.array(z.string()).optional(), "required_evidence": z.array(z.string()).optional() }).strict().describe("Gareng's feasibility and risk review, present on gareng-review records. See §8.2.").optional(), "petruk_plan": z.object({ "recommended_solution": z.string().optional(), "alternatives": z.array(z.string()).optional(), "tradeoffs": z.array(z.string()).optional(), "implementation_steps": z.array(z.string()).optional(), "repository_changes": z.array(z.string()).optional(), "test_plan": z.array(z.string()).optional(), "e2e_plan": z.array(z.string()).optional(), "deployment_plan": z.array(z.string()).optional(), "documentation_plan": z.array(z.string()).optional() }).strict().describe("Petruk's usefulness challenge and implementation planning output, present on petruk-plan records. See §8.3.").optional(), "bagong_review": z.object({ "verdict": z.string().optional(), "requirement_coverage": z.array(z.string()).optional(), "findings": z.array(z.string()).optional(), "blocking_findings": z.array(z.string()).optional(), "test_gaps": z.array(z.string()).optional(), "security_findings": z.array(z.string()).optional(), "compatibility_findings": z.array(z.string()).optional(), "uncertainties": z.array(z.string()).optional(), "honest_summary": z.string().optional() }).strict().describe("Bagong's independent final review, present on bagong-review records. See §8.4.").optional(), "final_plan": z.object({ "requirements": z.array(z.string()).optional(), "acceptance_criteria": z.array(z.string()).optional(), "non_goals": z.array(z.string()).optional(), "architecture_decision": z.string().optional(), "data_model_impact": z.string().optional(), "api_impact": z.string().optional(), "repository_impact_map": z.record(z.string(), z.string()).optional(), "implementation_sequence": z.array(z.string()).optional(), "unit_test_plan": z.array(z.string()).optional(), "integration_test_plan": z.array(z.string()).optional(), "e2e_plan": z.array(z.string()).optional(), "migration_plan": z.array(z.string()).optional(), "rollback_plan": z.array(z.string()).optional(), "observability_plan": z.array(z.string()).optional(), "documentation_plan": z.array(z.string()).optional(), "deployment_changes": z.array(z.string()).optional(), "security_considerations": z.array(z.string()).optional(), "compatibility_considerations": z.array(z.string()).optional(), "verification_criteria": z.array(z.string()).optional(), "risks_and_mitigations": z.array(z.string()).optional() }).strict().describe("Semar's final implementation plan, present on final-plan records. See §9.3.").optional(), "retrieval_recipe": z.object({ "capability": z.string().describe("Capability registry identifier this recipe is bound to, e.g. jira.issue.search."), "intent": z.string().describe("Typed operation intent this recipe answers, e.g. project.next-sprint.issues."), "provider": z.string(), "resource": z.string(), "operation": z.string(), "read_only": z.literal(true).describe("Must be true for every recipe this phase's engine can execute (§14: a future write-capable ActionRecipe is a separate type, gated by the approval engine, not a flag here)."), "recipe_version": z.number().int().gte(1).describe("Monotonically increasing per capability+intent+scope lineage; incremented on every accepted correction.").optional(), "applies_to": z.object({ "workspace_ids": z.array(z.string()).optional(), "repository_ids": z.array(z.string()).optional() }).strict().optional(), "inputs": z.array(z.object({ "name": z.string(), "type": z.string(), "required": z.boolean().optional(), "default": z.string().optional() }).strict()).optional(), "selector": z.object({ "all": z.array(z.object({ "field": z.string().optional(), "operator": z.enum(["equals","not_equals","phrase_contains","contains","in","not_in","greater_than","less_than"]).optional(), "value": z.any().superRefine((x, ctx) => {
    const schemas = [z.object({ "literal": z.union([z.string(), z.number(), z.boolean()]) }).strict(), z.object({ "resolver": z.string(), "arguments": z.record(z.string(), z.any()).optional() }).strict()];
    const { errors, failed } = schemas.reduce<{
      errors: z.core.$ZodIssue[];
      failed: number;
    }>(
      ({ errors, failed }, schema) =>
        ((result) =>
          result.error
            ? {
                errors: [...errors, ...result.error.issues],
                failed: failed + 1,
              }
            : { errors, failed })(
          schema.safeParse(x),
        ),
      { errors: [], failed: 0 },
    );
    const passed = schemas.length - failed;
    if (passed !== 1) {
      ctx.addIssue(errors.length ? {
        path: [],
        code: "invalid_union",
        errors: [errors],
        message: "Invalid input: Should pass single schema. Passed " + passed,
      } : {
        path: [],
        code: "custom",
        errors: [errors],
        message: "Invalid input: Should pass single schema. Passed " + passed,
      });
    }
  }).optional(), "all": z.array(z.object({ "field": z.string().optional(), "operator": z.enum(["equals","not_equals","phrase_contains","contains","in","not_in","greater_than","less_than"]).optional(), "value": z.any().superRefine((x, ctx) => {
    const schemas = [z.object({ "literal": z.union([z.string(), z.number(), z.boolean()]) }).strict(), z.object({ "resolver": z.string(), "arguments": z.record(z.string(), z.any()).optional() }).strict()];
    const { errors, failed } = schemas.reduce<{
      errors: z.core.$ZodIssue[];
      failed: number;
    }>(
      ({ errors, failed }, schema) =>
        ((result) =>
          result.error
            ? {
                errors: [...errors, ...result.error.issues],
                failed: failed + 1,
              }
            : { errors, failed })(
          schema.safeParse(x),
        ),
      { errors: [], failed: 0 },
    );
    const passed = schemas.length - failed;
    if (passed !== 1) {
      ctx.addIssue(errors.length ? {
        path: [],
        code: "invalid_union",
        errors: [errors],
        message: "Invalid input: Should pass single schema. Passed " + passed,
      } : {
        path: [],
        code: "custom",
        errors: [errors],
        message: "Invalid input: Should pass single schema. Passed " + passed,
      });
    }
  }).optional() }).strict()).optional(), "any": z.array(z.object({ "field": z.string().optional(), "operator": z.enum(["equals","not_equals","phrase_contains","contains","in","not_in","greater_than","less_than"]).optional(), "value": z.any().superRefine((x, ctx) => {
    const schemas = [z.object({ "literal": z.union([z.string(), z.number(), z.boolean()]) }).strict(), z.object({ "resolver": z.string(), "arguments": z.record(z.string(), z.any()).optional() }).strict()];
    const { errors, failed } = schemas.reduce<{
      errors: z.core.$ZodIssue[];
      failed: number;
    }>(
      ({ errors, failed }, schema) =>
        ((result) =>
          result.error
            ? {
                errors: [...errors, ...result.error.issues],
                failed: failed + 1,
              }
            : { errors, failed })(
          schema.safeParse(x),
        ),
      { errors: [], failed: 0 },
    );
    const passed = schemas.length - failed;
    if (passed !== 1) {
      ctx.addIssue(errors.length ? {
        path: [],
        code: "invalid_union",
        errors: [errors],
        message: "Invalid input: Should pass single schema. Passed " + passed,
      } : {
        path: [],
        code: "custom",
        errors: [errors],
        message: "Invalid input: Should pass single schema. Passed " + passed,
      });
    }
  }).optional() }).strict()).optional() }).strict()).optional(), "any": z.array(z.object({ "field": z.string().optional(), "operator": z.enum(["equals","not_equals","phrase_contains","contains","in","not_in","greater_than","less_than"]).optional(), "value": z.any().superRefine((x, ctx) => {
    const schemas = [z.object({ "literal": z.union([z.string(), z.number(), z.boolean()]) }).strict(), z.object({ "resolver": z.string(), "arguments": z.record(z.string(), z.any()).optional() }).strict()];
    const { errors, failed } = schemas.reduce<{
      errors: z.core.$ZodIssue[];
      failed: number;
    }>(
      ({ errors, failed }, schema) =>
        ((result) =>
          result.error
            ? {
                errors: [...errors, ...result.error.issues],
                failed: failed + 1,
              }
            : { errors, failed })(
          schema.safeParse(x),
        ),
      { errors: [], failed: 0 },
    );
    const passed = schemas.length - failed;
    if (passed !== 1) {
      ctx.addIssue(errors.length ? {
        path: [],
        code: "invalid_union",
        errors: [errors],
        message: "Invalid input: Should pass single schema. Passed " + passed,
      } : {
        path: [],
        code: "custom",
        errors: [errors],
        message: "Invalid input: Should pass single schema. Passed " + passed,
      });
    }
  }).optional(), "all": z.array(z.object({ "field": z.string().optional(), "operator": z.enum(["equals","not_equals","phrase_contains","contains","in","not_in","greater_than","less_than"]).optional(), "value": z.any().superRefine((x, ctx) => {
    const schemas = [z.object({ "literal": z.union([z.string(), z.number(), z.boolean()]) }).strict(), z.object({ "resolver": z.string(), "arguments": z.record(z.string(), z.any()).optional() }).strict()];
    const { errors, failed } = schemas.reduce<{
      errors: z.core.$ZodIssue[];
      failed: number;
    }>(
      ({ errors, failed }, schema) =>
        ((result) =>
          result.error
            ? {
                errors: [...errors, ...result.error.issues],
                failed: failed + 1,
              }
            : { errors, failed })(
          schema.safeParse(x),
        ),
      { errors: [], failed: 0 },
    );
    const passed = schemas.length - failed;
    if (passed !== 1) {
      ctx.addIssue(errors.length ? {
        path: [],
        code: "invalid_union",
        errors: [errors],
        message: "Invalid input: Should pass single schema. Passed " + passed,
      } : {
        path: [],
        code: "custom",
        errors: [errors],
        message: "Invalid input: Should pass single schema. Passed " + passed,
      });
    }
  }).optional() }).strict()).optional(), "any": z.array(z.object({ "field": z.string().optional(), "operator": z.enum(["equals","not_equals","phrase_contains","contains","in","not_in","greater_than","less_than"]).optional(), "value": z.any().superRefine((x, ctx) => {
    const schemas = [z.object({ "literal": z.union([z.string(), z.number(), z.boolean()]) }).strict(), z.object({ "resolver": z.string(), "arguments": z.record(z.string(), z.any()).optional() }).strict()];
    const { errors, failed } = schemas.reduce<{
      errors: z.core.$ZodIssue[];
      failed: number;
    }>(
      ({ errors, failed }, schema) =>
        ((result) =>
          result.error
            ? {
                errors: [...errors, ...result.error.issues],
                failed: failed + 1,
              }
            : { errors, failed })(
          schema.safeParse(x),
        ),
      { errors: [], failed: 0 },
    );
    const passed = schemas.length - failed;
    if (passed !== 1) {
      ctx.addIssue(errors.length ? {
        path: [],
        code: "invalid_union",
        errors: [errors],
        message: "Invalid input: Should pass single schema. Passed " + passed,
      } : {
        path: [],
        code: "custom",
        errors: [errors],
        message: "Invalid input: Should pass single schema. Passed " + passed,
      });
    }
  }).optional() }).strict()).optional() }).strict()).optional() }).strict().describe("Structured selector AST (§5): a group of clauses. Bounded at two nesting levels - a clause may itself be a nested any/all group, but that group's own clauses are leaves only (field+operator+value, no further any/all). The real Jira next-sprint example never nests deeper than this; a provider needing deeper nesting is a schema revision for a later phase, not a silent extension here. No $ref/$defs is used anywhere in this repo's schemas yet, so the leaf clause shape is duplicated inline (all/any/leaf) rather than risk being the first to rely on untested go-jsonschema $ref support."), "ordering": z.array(z.object({ "field": z.string(), "direction": z.enum(["ascending","descending"]) }).strict()).optional(), "output": z.object({ "entity_type": z.string(), "identity_field": z.string(), "fields": z.array(z.string()) }).strict(), "validation": z.object({ "status": z.enum(["pending","passed","failed"]).optional(), "validation_id": z.string().optional(), "provider_instance_fingerprint": z.string().optional(), "compiled_query_hash": z.string().regex(new RegExp("^sha256:[0-9a-f]{64}$")).optional(), "sample_size": z.number().int().optional(), "accepted_result_count": z.number().int().optional(), "accepted_by": z.string().optional(), "accepted_at": z.string().datetime({ offset: true }).optional(), "evidence_ids": z.array(z.string()).optional() }).strict().describe("The most recent validation/acceptance report (§9, §4's validation: block). Sample results and full compiled-query text are stored as evidence records, referenced by id here, not inlined.").optional(), "last_execution": z.object({ "session_id": z.string().optional(), "task_id": z.string().optional(), "executed_at": z.string().datetime({ offset: true }).optional(), "bindings": z.record(z.string(), z.any()).optional(), "compiled_query_hash": z.string().optional(), "result_count": z.number().int().optional(), "provider_request_id": z.string().optional(), "status": z.enum(["success","failure"]).optional(), "evidence_id": z.string().optional() }).strict().describe("The most recent execution's evidence summary (§13). Full history lives in evidence records referenced by evidence_id; this is only the latest, for quick display.").optional() }).strict().describe("A declarative, provider-tested procedure for a recurring read-only lookup, present on retrieval-recipe records. See punakawan-procedural-knowledge-retrieval-recipe-plan-final.md §3-4. Lifecycle state lives in validity.state (reusing the shared enum, not duplicated here); this object holds recipe-specific binding, selector, and evidence data only. Immutable versioning is achieved the same way every other knowledge record is corrected: a new id/version is Put and the old one's superseded_by is set, with relations[{type:supersedes,target:<old-id>}] recording the forward link - no separate versioning mechanism is introduced.").optional() }).strict().describe("A durable knowledge record with provenance and validity state. See punakawan-go-typescript-detailed-plan.md §7.")
export type KnowledgeRecordSchema = z.infer<typeof KnowledgeRecordSchema>
