import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "../src/App.svelte";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return {
    ok,
    status,
    json: async () => body,
  } as Response;
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("App", () => {
  it("shows the empty state when no workspaces are registered", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockImplementation((url: string) => {
      if (url.includes("/system")) {
        return Promise.resolve(
          jsonResponse({
            panel_version: "0.1.0",
            punakawan_version: "0.1.0",
            server_start_time: "2026-07-23T00:00:00Z",
            read_only: true,
            bound_address: "127.0.0.1:7331",
            registered_workspaces: 0,
            watcher_status: "not_implemented",
            feature_flags: [],
          }),
        );
      }
      return Promise.resolve(jsonResponse({ items: [] }));
    });

    render(App);

    await waitFor(() => {
      expect(screen.getByText(/No Punakawan workspaces are registered/i)).toBeTruthy();
    });
    expect(screen.getByTestId("read-only-badge").textContent).toContain("Read-only");
  });

  it("lists registered workspaces", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockImplementation((url: string) => {
      if (url.includes("/system")) {
        return Promise.resolve(
          jsonResponse({
            panel_version: "0.1.0",
            punakawan_version: "0.1.0",
            server_start_time: "2026-07-23T00:00:00Z",
            read_only: true,
            bound_address: "127.0.0.1:7331",
            registered_workspaces: 1,
            watcher_status: "not_implemented",
            feature_flags: [],
          }),
        );
      }
      return Promise.resolve(
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
              pinned: false,
            },
          ],
        }),
      );
    });

    render(App);

    await waitFor(() => {
      expect(screen.getByText("Checkout Platform")).toBeTruthy();
    });
    expect(screen.getByText("/repos/checkout-platform")).toBeTruthy();
  });

  it("shows an error state when the API call fails", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockImplementation(() =>
      Promise.resolve(jsonResponse({ error: "boom" }, false, 500)),
    );

    render(App);

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toContain("boom");
    });
  });
});
