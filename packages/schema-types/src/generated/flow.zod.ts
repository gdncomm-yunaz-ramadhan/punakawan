/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

import { z } from "zod"

export const BrowserFlowSchema = z.object({ "id": z.string(), "name": z.string(), "version": z.number().int().gte(1), "purpose": z.object({ "actor": z.string(), "goal": z.string() }).strict(), "preconditions": z.array(z.string()).optional(), "steps": z.array(z.object({ "id": z.string(), "action": z.enum(["navigate","click","fill","select","check","uncheck","submit","keyboard"]), "route": z.string().optional(), "target": z.object({ "role": z.string().optional(), "name": z.string().optional(), "name_pattern": z.string().optional(), "label": z.string().optional(), "placeholder": z.string().optional(), "text": z.string().optional() }).strict().optional(), "value": z.any().superRefine((x, ctx) => {
    const schemas = [z.string(), z.object({ "kind": z.literal("secret"), "parameter": z.string().optional(), "recorded": z.literal(false).optional() }).strict(), z.object({ "parameter": z.string() }).strict()];
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
  }).optional() }).strict()).min(1), "expected": z.array(z.object({ "type": z.enum(["text_visible","url_matches","element_visible"]), "value": z.string() }).strict()).optional(), "relations": z.array(z.object({ "type": z.enum(["validates","automated-by"]), "target": z.string() }).strict()).optional() }).strict().describe("A normalized, semantic browser flow recorded by the human-guided recorder. See punakawan-go-typescript-detailed-plan.md §12.9.")
export type BrowserFlowSchema = z.infer<typeof BrowserFlowSchema>
