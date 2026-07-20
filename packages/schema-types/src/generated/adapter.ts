/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * Manifest describing a TypeScript adapter's identity, transport, capabilities, and permissions. See punakawan-go-typescript-detailed-plan.md §5.4.
 */
export interface AdapterManifest {
  id: string;
  name: string;
  version: string;
  protocol: "punakawan.adapter/v1";
  runtime: "node";
  /**
   * @minItems 1
   */
  provides: [string, ...string[]];
  permissions: {
    network: {
      hosts: string[];
    };
    filesystem: {
      read: string[];
      write: string[];
    };
    secrets: string[];
  };
  operations: {
    [k: string]: {
      side_effect: boolean;
      approval?: "required";
    };
  };
}
