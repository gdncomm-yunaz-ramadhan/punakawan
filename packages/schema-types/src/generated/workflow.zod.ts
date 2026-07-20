/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const WorkflowRunSchema = z.object({ "id": z.string(), "workspace": z.string(), "workflow_name": z.enum(["feature-delivery","requirement-review","browser-flow-capture","implementation-only","final-review"]), "state": z.enum(["created","context-building","awaiting-clarification","planning","awaiting-approval","executing","reviewing","blocked","completed","failed","cancelled"]), "created_at": z.string().datetime({ offset: true }), "updated_at": z.string().datetime({ offset: true }), "checkpoints": z.array(z.object({ "state": z.string(), "at": z.string().datetime({ offset: true }), "note": z.string().optional() }).strict()).optional() }).strict().describe("A running or completed workflow instance and its state-machine position. See punakawan-go-typescript-detailed-plan.md §9, §18.1.")
export type WorkflowRunSchema = z.infer<typeof WorkflowRunSchema>
