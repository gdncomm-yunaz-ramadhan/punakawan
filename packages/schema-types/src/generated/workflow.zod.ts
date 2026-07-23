/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const WorkflowRunSchema = z.object({ "id": z.string(), "workspace": z.string(), "workflow_name": z.enum(["feature-delivery","requirement-review","browser-flow-capture","implementation-only","final-review"]), "state": z.enum(["created","context-building","awaiting-clarification","planning","awaiting-approval","executing","reviewing","blocked","completed","failed","cancelled"]), "created_at": z.string().datetime({ offset: true }), "updated_at": z.string().datetime({ offset: true }), "objective": z.string().describe("Human-readable goal of this run, set by the calling agent at creation or advance time. Used by the panel's session summary (punakawan-panel-implementation-plan.md §8.3); Punakawan never infers or edits this itself.").optional(), "initiator": z.string().describe("Who or what started this run (e.g. \"user\", \"scheduled\", an agent identifier). Set by the calling agent, not inferred.").optional(), "active_role": z.enum(["semar","gareng","petruk","bagong"]).describe("The Punakawan role currently driving this run, as reported by the calling agent.").optional(), "checkpoints": z.array(z.object({ "state": z.string(), "at": z.string().datetime({ offset: true }), "note": z.string().optional() }).strict()).optional() }).strict().describe("A running or completed workflow instance and its state-machine position. See punakawan-go-typescript-detailed-plan.md §9, §18.1.")
export type WorkflowRunSchema = z.infer<typeof WorkflowRunSchema>
