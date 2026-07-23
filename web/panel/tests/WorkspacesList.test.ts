import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import WorkspacesList from "../src/routes/workspaces/WorkspacesList.svelte";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("WorkspacesList", () => {
  it("shows the empty state when no workspaces are registered", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ items: [] }));

    render(WorkspacesList);

    await waitFor(() => {
      expect(screen.getByText(/No Punakawan workspaces are registered/i)).toBeTruthy();
    });
  });

  it("lists registered workspaces with their status badge", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({
        items: [
          {
            id: "checkout-platform",
            path: "/repos/checkout-platform",
            display_name: "Checkout Platform",
            availability: "available",
            repository_count: 1,
            active_session_count: 2,
            open_task_count: 5,
            blocked_task_count: 1,
            knowledge_count: 10,
            last_activity_at: "2026-07-23T00:00:00Z",
            pinned: true,
          },
        ],
      }),
    );

    render(WorkspacesList);

    await waitFor(() => {
      expect(screen.getByText("Checkout Platform")).toBeTruthy();
    });
    expect(screen.getByText("/repos/checkout-platform")).toBeTruthy();
    expect(screen.getByText("Available")).toBeTruthy();
  });

  it("shows an error state when the API call fails", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ error: "boom" }, false, 500));

    render(WorkspacesList);

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toContain("boom");
    });
  });
});
