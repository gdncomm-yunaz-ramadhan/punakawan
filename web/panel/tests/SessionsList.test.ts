import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SessionsList from "../src/routes/sessions/SessionsList.svelte";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("SessionsList", () => {
  it("shows the empty state when a workspace has no sessions", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ items: [] }));

    render(SessionsList, { props: { workspaceId: "ws-a" } });

    await waitFor(() => {
      expect(screen.getByText("No sessions yet.")).toBeTruthy();
    });
  });

  it("lists sessions with their status and role", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({
        items: [
          {
            id: "run-1",
            workspace_id: "ws-a",
            workflow: "feature-delivery",
            status: "executing",
            started_at: "2026-07-23T00:00:00Z",
            updated_at: "2026-07-23T00:00:00Z",
            active_role: "petruk",
          },
        ],
      }),
    );

    render(SessionsList, { props: { workspaceId: "ws-a" } });

    await waitFor(() => {
      expect(screen.getByText("run-1")).toBeTruthy();
    });
    expect(screen.getByText("feature-delivery")).toBeTruthy();
    expect(screen.getByText("petruk")).toBeTruthy();
  });

  it("shows an error state when the API call fails", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ error: "boom" }, false, 500));

    render(SessionsList, { props: { workspaceId: "ws-a" } });

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toContain("boom");
    });
  });
});
