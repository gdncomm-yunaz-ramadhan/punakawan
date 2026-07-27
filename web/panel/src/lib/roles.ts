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
