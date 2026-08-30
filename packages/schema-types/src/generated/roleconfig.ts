/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * Per-project prompt preferences for the four Punakawan roles (Semar coordinates, Gareng challenges, Petruk plans and builds, Bagong verifies). Persisted at .punakawan/roles.yaml. Style selects one of three fixed prompt-guidance strings appended to a role's prompt; instructions is free text appended after that guidance. This configuration only shapes prompt wording: it never authorizes tools, grants permissions, or changes what a workflow requires. revision is bumped on every save for optimistic locking.
 */
export interface RolePreferences {
  version: 2;
  revision: number;
  roles: {
    semar: RolePreference;
    gareng: RolePreference;
    petruk: RolePreference;
    bagong: RolePreference;
  };
}
export interface RolePreference {
  /**
   * Selects one of three fixed prompt-guidance strings appended to the role's prompt. Never changes tool access or workflow requirements.
   */
  style: "strict" | "balanced" | "creative";
  /**
   * Free-text guidance appended after the fixed style guidance, bounded to 2000 characters.
   */
  instructions: string;
}
