/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const AdapterManifestSchema = z.object({ "id": z.string().regex(new RegExp("^[a-z0-9]+(-[a-z0-9]+)*$")), "name": z.string(), "version": z.string().regex(new RegExp("^\\d+\\.\\d+\\.\\d+(-[0-9A-Za-z.-]+)?$")), "protocol": z.literal("punakawan.adapter/v1"), "runtime": z.literal("node"), "provides": z.array(z.string()).min(1), "permissions": z.object({ "network": z.object({ "hosts": z.array(z.string()) }).strict(), "filesystem": z.object({ "read": z.array(z.string()), "write": z.array(z.string()) }).strict(), "secrets": z.array(z.string()) }).strict(), "operations": z.record(z.string(), z.object({ "side_effect": z.boolean(), "approval": z.literal("required").optional() }).strict()) }).strict().describe("Manifest describing a TypeScript adapter's identity, transport, capabilities, and permissions. See punakawan-go-typescript-detailed-plan.md §5.4.")
export type AdapterManifestSchema = z.infer<typeof AdapterManifestSchema>
