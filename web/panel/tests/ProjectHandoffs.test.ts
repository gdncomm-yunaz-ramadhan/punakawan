import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectHandoffs from "../src/routes/projects/ProjectHandoffs.svelte";
import { setCsrfToken } from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

type FetchMock = ReturnType<typeof vi.fn>;

const listPayload = {
  items: [
    {
      id: "h1",
      run_id: "run-42",
      current_phase: "implementation",
      objective: { statement: "Wire up OAuth" },
      current_task: { id: "t9", next_action: "Add callback handler" },
      created_by: { role: "petruk", agent_client: "claude-code" },
      created_at: "2026-07-22T09:00:00Z",
    },
  ],
};

const validationPayload = {
  status: "refresh_required",
  changes_since_handoff: ["repo-auth advanced 3 commits"],
  required_refresh: ["knowledge:auth-flow"],
};

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  setCsrfToken("csrf-test-token");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ProjectHandoffs", () => {
  it("renders a card per handoff capsule", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse(listPayload));
    render(ProjectHandoffs, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByTestId("handoff-h1")).toBeTruthy());
    const card = within(screen.getByTestId("handoff-h1"));
    expect(card.getByText("Wire up OAuth")).toBeTruthy();
    expect(card.getByText(/Add callback handler/)).toBeTruthy();
  });

  it("shows validation status and lists after Validate", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (url: string, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "POST" && url.includes("/validate")) return jsonResponse(validationPayload);
      return jsonResponse(listPayload);
    });
    render(ProjectHandoffs, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByTestId("handoff-h1")).toBeTruthy());
    await fireEvent.click(screen.getByRole("button", { name: "Validate" }));

    await waitFor(() => expect(screen.getByTestId("handoff-validation-h1")).toBeTruthy());
    const v = within(screen.getByTestId("handoff-validation-h1"));
    expect(v.getByText("Refresh required")).toBeTruthy();
    expect(v.getByText("repo-auth advanced 3 commits")).toBeTruthy();
    expect(v.getByText("knowledge:auth-flow")).toBeTruthy();
  });
});
