import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectRoles from "../src/routes/projects/ProjectRoles.svelte";
import { setCsrfToken } from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

type FetchMock = ReturnType<typeof vi.fn>;

// The Defaults-shaped payload from the backend contract: four roles, each
// with a style and free-text instructions. Every role starts balanced with
// empty instructions.
function defaultsPayload() {
  const rolePreference = () => ({ style: "balanced", instructions: "" });
  return {
    roles: {
      semar: rolePreference(),
      gareng: rolePreference(),
      petruk: rolePreference(),
      bagong: rolePreference(),
    },
    revision: 7,
  };
}

// A tiny in-memory backend: GET returns the current payload; PATCH/POST
// bump the revision and echo the (possibly mutated) role map.
function installBackend(opts: { onMutate?: () => Response } = {}) {
  const state = defaultsPayload();
  (fetch as unknown as FetchMock).mockImplementation(async (url: string, init?: RequestInit) => {
    const method = (init?.method ?? "GET").toUpperCase();
    if (method === "GET") {
      return jsonResponse({ roles: state.roles, revision: state.revision });
    }
    if (opts.onMutate) return opts.onMutate();
    // PATCH /roles/{role}  or POST /roles/{role}/reset
    const role = decodeURIComponent((url.split("/roles/")[1] ?? "").split(/[/?]/)[0]);
    const body = JSON.parse(init!.body as string);
    if (method === "PATCH") {
      (state.roles as Record<string, unknown>)[role] = {
        style: body.style,
        instructions: body.instructions,
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

  it("renders only style and instructions fields, with no enabled/mode/capability controls", async () => {
    installBackend();
    render(ProjectRoles, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByLabelText("Semar role")).toBeTruthy());

    const semar = within(screen.getByLabelText("Semar role"));
    expect(semar.getByRole("radiogroup")).toBeTruthy();
    expect(semar.getByRole("radio", { name: "Strict" })).toBeTruthy();
    expect(semar.getByRole("radio", { name: "Balanced" })).toBeTruthy();
    expect(semar.getByRole("radio", { name: "Creative" })).toBeTruthy();
    expect(semar.getByLabelText("Instructions")).toBeTruthy();

    // No mode/enabled/capability controls anywhere on the screen.
    expect(screen.queryByLabelText(/Enable /)).toBeNull();
    expect(screen.queryByRole("radio", { name: "Assist" })).toBeNull();
    expect(screen.queryByRole("radio", { name: "Propose" })).toBeNull();
    expect(screen.queryByRole("radio", { name: "Execute" })).toBeNull();
  });

  it("keeps Save disabled until a control changes, then saves style and instructions with the loaded revision", async () => {
    installBackend();
    render(ProjectRoles, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByLabelText("Semar role")).toBeTruthy());

    const semar = within(screen.getByLabelText("Semar role"));
    const saveBtn = () => semar.getByRole("button", { name: "Save" });

    // Save starts disabled (card is not dirty).
    expect(saveBtn().hasAttribute("disabled")).toBe(true);

    // Pick a different style -> card becomes dirty -> Save enables.
    await fireEvent.click(semar.getByRole("radio", { name: "Strict" }));
    await fireEvent.input(semar.getByLabelText("Instructions"), { target: { value: "Prefer reversible migrations." } });
    await waitFor(() => expect(saveBtn().hasAttribute("disabled")).toBe(false));

    await fireEvent.click(saveBtn());

    await waitFor(() => {
      const patch = (fetch as unknown as FetchMock).mock.calls.find((c) => c[1]?.method === "PATCH");
      expect(patch).toBeTruthy();
      expect(patch![0]).toContain("/roles/semar");
      const body = JSON.parse(patch![1].body);
      expect(body.base_revision).toBe(7);
      expect(body.style).toBe("strict");
      expect(body.instructions).toBe("Prefer reversible migrations.");
    });
  });
});
