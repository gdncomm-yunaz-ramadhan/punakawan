/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * A normalized, semantic browser flow recorded by the human-guided recorder. See punakawan-go-typescript-detailed-plan.md §12.9.
 */
export interface BrowserFlow {
  id: string;
  name: string;
  version: number;
  purpose: {
    actor: string;
    goal: string;
  };
  preconditions?: string[];
  /**
   * @minItems 1
   */
  steps: [
    {
      id: string;
      action: "navigate" | "click" | "fill" | "select" | "check" | "uncheck" | "submit" | "keyboard";
      route?: string;
      target?: {
        role?: string;
        name?: string;
        name_pattern?: string;
        label?: string;
        placeholder?: string;
        text?: string;
      };
      value?:
        | string
        | {
            kind: "secret";
            parameter?: string;
            recorded?: false;
          }
        | {
            parameter: string;
          };
    },
    ...{
      id: string;
      action: "navigate" | "click" | "fill" | "select" | "check" | "uncheck" | "submit" | "keyboard";
      route?: string;
      target?: {
        role?: string;
        name?: string;
        name_pattern?: string;
        label?: string;
        placeholder?: string;
        text?: string;
      };
      value?:
        | string
        | {
            kind: "secret";
            parameter?: string;
            recorded?: false;
          }
        | {
            parameter: string;
          };
    }[]
  ];
  expected?: {
    type: "text_visible" | "url_matches" | "element_visible";
    value: string;
  }[];
  relations?: {
    type: "validates" | "automated-by";
    target: string;
  }[];
}
