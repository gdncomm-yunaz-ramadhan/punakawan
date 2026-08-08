/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const TestReportSummarySchema = z.object({ "command": z.string(), "exit_code": z.number().int(), "duration_ms": z.number().int().gte(0), "total_tests": z.number().int().gte(0).optional(), "passed": z.number().int().gte(0).optional(), "failed": z.number().int().gte(0).optional(), "skipped": z.number().int().gte(0).optional(), "first_causal_failure": z.string().describe("The deepest \"Caused by:\" line, or the first recognized failure signature, extracted from the combined output.").optional(), "tail": z.string().describe("Bounded, retry-noise-deduplicated tail of combined stdout+stderr."), "truncated": z.boolean(), "artifact_id": z.string().describe("EvidenceArtifact id of the full, untruncated combined log.") }).strict().describe("A bounded, causal-first projection of one test invocation's output (punokawan-14yn.7), concise enough for a continuous agent loop while the full stdout/stderr remains available, unmodified, as the referenced EvidenceArtifact. Never returned instead of the full log - only alongside it.")
export type TestReportSummarySchema = z.infer<typeof TestReportSummarySchema>
