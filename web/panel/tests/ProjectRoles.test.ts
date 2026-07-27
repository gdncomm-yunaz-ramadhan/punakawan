import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectRoles from "../src/routes/projects/ProjectRoles.svelte";
import { setCsrfToken } from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

type FetchMock = ReturnType<typeof vi.fn>;

// The Defaults-shaped payload from the backend contract: four roles, each
// with the capability keys it owns. Every role starts enabled/balanced with
// all owned capabilities on.
function defaultsPayload() {
  const owned = [
    { role: "semar", capabilities: ["workflows", "clarification", "coordinate_roles", "change_dossier", "handoff_capsule"] },
    { role: "gareng", capabilities: ["contradictions", "cross_repository_impact", "security_checks", "blocking_risks", "change_dossier"] },
    { role: "petruk", capabilities: ["plans", "tasks", "modify_files", "cross_repository_changes", "create_pull_request", "change_dossier"] },
    { role: "bagong", capabilities: ["plan_verification", "rerun_checks", "cross_repository_verification", "challenge_dossier", "block_completion", "review_pull_request"] },
  ];
  const roleConfig = (mode: string, caps: string[]) => ({
    enabled: true,
    style: "balanced",
    mode,
    capabilities: Object.fromEntries(caps.map((k) => [k, true])),
  });
  return {
    roles: {
      semar: roleConfig("execute", owned[0].capabilities),
      gareng: roleConfig("propose", owned[1].capabilities),
      petruk: roleConfig("execute", owned[2].capabilities),
      bagong: roleConfig("propose", owned[3].capabilities),
    },
    revision: 7,
    owned,
  };
}

// A tiny in-memory backend: GET returns the current payload; PATCH/POST
// bump the revision and echo the (possibly mutated) role map.
function installBackend(opts: { onMutate?: () => Response } = {}) {
  const state = defaultsPayload();
  (fetch as unknown as FetchMock).mockImplementation(async (url: string, init?: RequestInit) => {
    const method = (init?.method ?? "GET").toUpperCase();
    if (method === "GET") {
      return jsonResponse({ roles: state.roles, revision: state.revision, owned: state.owned });
    }
    if (opts.onMutate) return opts.onMutate();
    // PATCH /roles/{role}  or POST /roles/{role}/reset
    const role = decodeURIComponent((url.split("/roles/")[1] ?? "").split(/[/?]/)[0]);
    const body = JSON.parse(init!.body as string);
    if (method === "PATCH") {
      (state.roles as Record<string, unknown>)[role] = {
        enabled: body.enabled,
        style: body.style,
        mode: body.mode,
        capabilities: body.capabilities,
      };
    }
    state.revision += 1;
    return jsonResponse({ roles: state.roles, revision: state.revision });
  });
  return state;
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  setCsrfToken("csrf-test-token");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ProjectRoles", () => {
  it("renders a card for all four roles", async () => {
    installBackend();
    render(ProjectRoles, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByLabelText("Semar role")).toBeTruthy());
    expect(screen.getByLabelText("Gareng role")).toBeTruthy();
    expect(screen.getByLabelText("Petruk role")).toBeTruthy();
    expect(screen.getByLabelText("Bagong role")).toBeTruthy();
  });

  it("shows each role's short principle and communication summary", async () => {
    installBackend();
    render(ProjectRoles, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByLabelText("Semar role")).toBeTruthy());

    const semar = within(screen.getByLabelText("Semar role"));
    expect(semar.getByText("Ground the work.")).toBeTruthy();
    expect(semar.getByText(/Calm and purpose-oriented/)).toBeTruthy();

    const gareng = within(screen.getByLabelText("Gareng role"));
    expect(gareng.getByText("Notice what others miss.")).toBeTruthy();

    const petruk = within(screen.getByLabelText("Petruk role"));
    expect(petruk.getByText("Make the idea useful.")).toBeTruthy();

    const bagong = within(screen.getByLabelText("Bagong role"));
    expect(bagong.getByText("Say what is true.")).toBeTruthy();
    expect(bagong.getByText(/Separates what was proven from what was merely claimed/)).toBeTruthy();
  });

  it("renders only the capability toggles a role owns", async () => {
    installBackend();
    render(ProjectRoles, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByLabelText("Semar role")).toBeTruthy());

    const petruk = within(screen.getByLabelText("Petruk role"));
    const gareng = within(screen.getByLabelText("Gareng role"));

    // Petruk owns "modify_files" -> "Modify files"; it renders inside
    // Petruk's card and NOT inside Gareng's.
    expect(petruk.queryByLabelText("Modify files")).not.toBeNull();
    expect(gareng.queryByLabelText("Modify files")).toBeNull();

    // Gareng owns "cross_repository_impact"; Petruk does not.
    expect(gareng.queryByLabelText("Cross repository impact")).not.toBeNull();
    expect(petruk.queryByLabelText("Cross repository impact")).toBeNull();
  });

  it("keeps Save disabled until a control changes, then saves with the loaded revision", async () => {
    installBackend();
    render(ProjectRoles, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByLabelText("Semar role")).toBeTruthy());

    const semar = within(screen.getByLabelText("Semar role"));
    const saveBtn = () => semar.getByRole("button", { name: "Save" });

    // Save starts disabled (card is not dirty).
    expect(saveBtn().hasAttribute("disabled")).toBe(true);

    // Flip one owned capability toggle -> card becomes dirty -> Save enables.
    await fireEvent.click(semar.getByLabelText("Workflows"));
    await waitFor(() => expect(saveBtn().hasAttribute("disabled")).toBe(false));

    await fireEvent.click(saveBtn());

    await waitFor(() => {
      const patch = (fetch as unknown as FetchMock).mock.calls.find((c) => c[1]?.method === "PATCH");
      expect(patch).toBeTruthy();
      expect(patch![0]).toContain("/roles/semar");
      const body = JSON.parse(patch![1].body);
      expect(body.base_revision).toBe(7);
      expect(body.capabilities.workflows).toBe(false);
    });
  });
});
