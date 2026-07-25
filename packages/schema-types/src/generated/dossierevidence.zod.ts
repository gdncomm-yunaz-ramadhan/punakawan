/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const DossierEvidenceSchema = z.object({ "id": z.string(), "dossier_id": z.string().optional(), "type": z.enum(["requirement_source","source_location","diff","test_result","build_result","api_compatibility","security_scan","dependency_scan","migration_check","screenshot","manual_confirmation","review_result"]), "source": z.object({ "command": z.string().optional(), "repository": z.string().optional(), "working_tree": z.string().optional(), "ref": z.string().optional() }).strict().optional(), "result": z.object({ "status": z.enum(["passed","failed","unknown"]).optional(), "exit_code": z.number().int().optional() }).strict().optional(), "artifacts": z.array(z.object({ "path": z.string(), "sha256": z.string().optional() }).strict()).optional() }).strict().describe("One evidence record within a Change Dossier, per punakawan-role-config-distinguished-improvements-plan.md §35. Records how a claim is backed: the command run (and where), its result, and any produced artifacts with content hashes. Distinct from internal task Evidence; this is the dossier's exportable, hash-anchored proof unit.")
export type DossierEvidenceSchema = z.infer<typeof DossierEvidenceSchema>
