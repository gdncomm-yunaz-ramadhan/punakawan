/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * Per-project configuration for the four Punakawan roles (Semar coordinates, Gareng challenges, Petruk plans and builds, Bagong verifies). Persisted at .punakawan/roles.yaml. See punakawan-role-config-distinguished-improvements-plan.md Part I. User-facing surface is deliberately small: enabled, style, mode, and a short list of role-specific capability toggles. Style changes reasoning behavior, not permissions; Mode gates whether a role may read (assist), propose (propose), or execute (execute) durable changes, always still constrained by workflow restrictions and project policy. revision is bumped on every save for optimistic locking.
 */
export interface RoleConfiguration {
  version: "punakawan.roles/v1";
  revision: number;
  roles: {
    semar: RoleConfig;
    gareng: RoleConfig;
    petruk: RoleConfig;
    bagong: RoleConfig;
  };
}
export interface RoleConfig {
  enabled: boolean;
  /**
   * Reasoning posture. strict: stronger evidence, fewer assumptions, blocks on unresolved issues more readily. balanced: reasonable assumptions, flags uncertainty without over-blocking. creative: explores more alternatives, searches more broadly. Never changes permissions.
   */
  style: "strict" | "balanced" | "creative";
  /**
   * Action ceiling. assist: read/search/analyze/report only, no durable state changes. propose: may create reviewable proposals, nothing applied automatically. execute: may perform enabled capabilities, still under policy/workflow constraints.
   */
  mode: "assist" | "propose" | "execute";
  /**
   * Role-specific capability toggles. Keys are validated against the set owned by the role (see internal/roleconfig defaults); a capability not owned by the role is rejected by the API.
   */
  capabilities: {
    [k: string]: boolean;
  };
}
