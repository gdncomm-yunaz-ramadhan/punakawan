/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const EvidenceArtifactSchema = z.object({ "id": z.string().describe("Filesystem-safe ULID (Crockford base32, 26 chars)."), "orchestration_id": z.string(), "project_id": z.string(), "lane_id": z.string().optional(), "parent_task_id": z.string().optional(), "kind": z.enum(["test","diff","api-check","command","screenshot","review","quality"]), "content_hash": z.string().regex(new RegExp("^sha256:[0-9a-f]{64}$")), "media_type": z.string(), "byte_size": z.number().int().gte(0), "producer": z.string().describe("What invoked this (e.g. \"go test\", \"petruk-plan\"), for provenance.").optional(), "created_at": z.string().datetime({ offset: true }), "retain_until": z.string().datetime({ offset: true }).describe("Retention horizon; absent means retained indefinitely.").optional() }).strict().describe("One immutable invocation record: a test, diff, API check, command, screenshot, or review produced exactly this content-addressed blob. Bytes are addressed by sha256 (never overwritten, never trusted from a caller - always server-computed); this record's own id is a ULID and is what gets referenced elsewhere, never a mutable path.")
export type EvidenceArtifactSchema = z.infer<typeof EvidenceArtifactSchema>
