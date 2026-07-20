/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const TaskContractSchema = z.object({ "id": z.string(), "requirement_id": z.string(), "jira_key": z.string().optional(), "beads_epic": z.string().optional(), "repository": z.string(), "dependencies": z.array(z.object({ "type": z.enum(["blocks","discovered-from","requires"]), "id": z.string() }).strict()).optional(), "scope": z.string(), "expected_files_or_components": z.array(z.string()).optional(), "acceptance_criteria": z.array(z.string()).min(1), "test_requirements": z.array(z.string()).optional(), "required_evidence": z.array(z.string()).optional(), "risk_classification": z.enum(["low","medium","high"]).optional(), "approval_required": z.boolean().optional(), "definition_of_done": z.string(), "discovered_from": z.string().optional() }).strict().describe("A dependency-aware Beads work item generated from an approved plan. See punakawan-go-typescript-detailed-plan.md §10.")
export type TaskContractSchema = z.infer<typeof TaskContractSchema>
