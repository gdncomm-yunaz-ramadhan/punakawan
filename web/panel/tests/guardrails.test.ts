import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectRoles from "../src/routes/projects/ProjectRoles.svelte";
import { setCsrfToken } from "../src/lib/session";
import { ROLE_META, ROLE_STEP_VERB, type RoleName } from "../src/lib/roles";

// Cultural guardrails (wayang-nuance plan): operational screens must stay
// professional. No faux-ancient or theatrical stage-character wording, no
// mystical terms, and no phrasing that portrays a role as unintelligent. The
// docs "inspiration" section may explain the wayang origin — it is not an
// operational screen and is not checked here.
const BANNED = [
  "clown",
  "puppet",
  "shadow-puppet",
  "jester",
  "buffoon",
  "fool",
  "mystical",
  "magic",
  "sorcery",
  "oracle",
  "prophecy",
  "spirit",
  "ancient",
  "thou",
  "thee",
  "thy",
  "hark",
  "sage",
  "unintelligent",
  "stupid",
  "simpleton",
];

function assertNoBannedWords(text: string, where: string) {
  const lower = text.toLowerCase();
  for (const term of BANNED) {
    // Word-boundary match so e.g. "management" does not trip "magic".
    const re = new RegExp(`\\b${term}\\b`, "i");
    expect(re.test(lower), `${where} contains banned wording "${term}": ${text}`).toBe(false);
  }
}

function jsonResponse(body: unknown) {
  return { ok: true, status: 200, json: async () => body } as Response;
}

function rolesPayload() {
  const owned = [
    { role: "semar", capabilities: ["workflows"] },
    { role: "gareng", capabilities: ["contradictions"] },
    { role: "petruk", capabilities: ["plans"] },
    { role: "bagong", capabilities: ["plan_verification"] },
  ];
  const rc = (mode: string, caps: string[]) => ({
    enabled: true,
    style: "balanced",
    mode,
    capabilities: Object.fromEntries(caps.map((k) => [k, true])),
  });
  return {
    roles: {
      semar: rc("execute", owned[0].capabilities),
      gareng: rc("propose", owned[1].capabilities),
      petruk: rc("execute", owned[2].capabilities),
      bagong: rc("propose", owned[3].capabilities),
    },
    revision: 1,
    owned,
  };
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn(async () => jsonResponse(rolesPayload())));
  setCsrfToken("csrf-test-token");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("cultural guardrails", () => {
  it("keeps the shared role copy free of theatrical/mystical wording", () => {
    for (const role of Object.keys(ROLE_META) as RoleName[]) {
      const m = ROLE_META[role];
      assertNoBannedWords(m.label, `ROLE_META.${role}.label`);
      assertNoBannedWords(m.principle, `ROLE_META.${role}.principle`);
      assertNoBannedWords(m.communication, `ROLE_META.${role}.communication`);
    }
    for (const [role, verb] of Object.entries(ROLE_STEP_VERB)) {
      assertNoBannedWords(verb, `ROLE_STEP_VERB.${role}`);
    }
  });

  it("keeps the rendered role screen free of theatrical/mystical wording", async () => {
    const { container } = render(ProjectRoles, { props: { projectId: "p1" } });
    await waitFor(() => expect(screen.getByLabelText("Semar role")).toBeTruthy());
    assertNoBannedWords(container.textContent ?? "", "ProjectRoles screen");
  });

  it("uses the approved short principle for each role", () => {
    expect(ROLE_META.semar.principle).toBe("Ground the work.");
    expect(ROLE_META.gareng.principle).toBe("Notice what others miss.");
    expect(ROLE_META.petruk.principle).toBe("Make the idea useful.");
    expect(ROLE_META.bagong.principle).toBe("Say what is true.");
  });
});
