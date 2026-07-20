/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const EvidenceRecordSchema = z.object({ "id": z.string(), "run_id": z.string(), "task_id": z.string().optional(), "type": z.enum(["source-excerpt","repository-snapshot","command-output","test-report","playwright-trace","screenshot","api-diff","git-diff","commit","user-answer","approval-record","external-response","review-finding"]), "path": z.string().optional(), "content_hash": z.string().regex(new RegExp("^sha256:[0-9a-f]{64}$")).optional(), "summary": z.string().optional(), "created_at": z.string().datetime({ offset: true }) }).strict().describe("A single piece of evidence linked to a task run and its supporting artifact. See punakawan-go-typescript-detailed-plan.md §17.")
export type EvidenceRecordSchema = z.infer<typeof EvidenceRecordSchema>
