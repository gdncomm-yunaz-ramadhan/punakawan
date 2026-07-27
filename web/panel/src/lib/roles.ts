// Shared presentation for the four Punakawan roles, so every view labels a
// role token consistently instead of printing a bare lowercase string. This is
// the single source for role display copy across session, overview, handoff,
// and workflow views.

export type RoleName = "semar" | "gareng" | "petruk" | "bagong";

export interface RoleMeta {
  label: string;
  principle: string;
  communication: string;
}

export const ROLE_META: Record<RoleName, RoleMeta> = {
  semar: {
    label: "Semar",
    principle: "Ground the work.",
    communication: "Calm and purpose-oriented. Summarizes decisions without hiding disagreement.",
  },
  gareng: {
    label: "Gareng",
    principle: "Notice what others miss.",
    communication: "Careful and evidence-backed. Focuses on meaningful consequences.",
  },
  petruk: {
    label: "Petruk",
    principle: "Make the idea useful.",
    communication: "Practical and solution-oriented. Prefers the simplest sufficient approach.",
  },
  bagong: {
    label: "Bagong",
    principle: "Say what is true.",
    communication: "Direct and plain. Separates what was proven from what was merely claimed.",
  },
};

export function isRoleName(token: string | null | undefined): token is RoleName {
  return token != null && token in ROLE_META;
}

/**
 * roleLabel maps a role token to its proper display label. A known role
 * (semar/gareng/petruk/bagong, case-insensitive) returns its label; any other
 * non-empty token is title-cased so it still reads cleanly; empty returns a
 * caller-supplied fallback (default "—").
 */
export function roleLabel(token: string | null | undefined, fallback = "—"): string {
  if (!token) return fallback;
  const key = token.toLowerCase();
  if (isRoleName(key)) return ROLE_META[key].label;
  return token.charAt(0).toUpperCase() + token.slice(1);
}

// ROLE_STEP_VERB is the ownership phrasing for a workflow step, per role.
export const ROLE_STEP_VERB: Record<RoleName, string> = {
  semar: "Grounded by",
  gareng: "Risk reviewed by",
  petruk: "Built by",
  bagong: "Verified by",
};

/**
 * stepRole infers which role owns a workflow step from its capability (the MCP
 * tool / capability name the step invokes), so the workflow view can show
 * role ownership instead of a bare capability. Returns null when no role
 * clearly owns the capability. Gareng/Bagong are checked before Petruk so an
 * explicit submit_*_review is not mistaken for generic execution.
 */
export function stepRole(capability: string): RoleName | null {
  const c = capability.toLowerCase();
  if (
    c.includes("semar") ||
    c.includes("synthesis") ||
    c.includes("final_plan") ||
    c.includes("clarification") ||
    c.includes("handoff") ||
    c.includes("coordinate")
  )
    return "semar";
  if (
    c.includes("gareng") ||
    c.includes("contradiction") ||
    c.includes("impact") ||
    c.includes("security")
  )
    return "gareng";
  if (
    c.includes("bagong") ||
    c.includes("verify") ||
    c.includes("verification") ||
    c.includes("review_pr") ||
    c.includes("rerun")
  )
    return "bagong";
  if (
    c.includes("petruk") ||
    c.includes("plan") ||
    c.includes("task") ||
    c.includes("write_file") ||
    c.includes("bulk_create") ||
    c.includes("run_tests") ||
    c.includes("check_diff") ||
    c.includes("commit") ||
    c.includes("create_pr") ||
    c.includes("push")
  )
    return "petruk";
  return null;
}
