import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SystemPage from "../src/routes/system/SystemPage.svelte";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("SystemPage", () => {
  it("renders the panel's system facts", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({
        panel_version: "0.1.0",
        punakawan_version: "0.1.0",
        server_start_time: "2026-07-23T00:00:00Z",
        read_only: true,
        bound_address: "127.0.0.1:7331",
        registered_workspaces: 2,
        watcher_status: "not_implemented",
        feature_flags: [],
      }),
    );

    render(SystemPage);

    await waitFor(() => {
      expect(screen.getByText("127.0.0.1:7331")).toBeTruthy();
    });
    expect(screen.getByText("2")).toBeTruthy();
    expect(screen.getByText("yes")).toBeTruthy();
  });
});
