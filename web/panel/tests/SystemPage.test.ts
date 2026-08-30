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
      }),
    );

    render(SystemPage);

    await waitFor(() => {
      expect(screen.getByText("127.0.0.1:7331")).toBeTruthy();
    });
    expect(screen.getByText("2")).toBeTruthy();
    expect(screen.getByText("yes")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Color accent" })).toBeTruthy();
    expect(screen.getByRole("radio", { name: "Cobalt" })).toBeTruthy();
    expect(screen.getByRole("radio", { name: "Teal" })).toBeTruthy();

    // The runtime cache has no cosmetic settings surface: no max-runtime or
    // idle-timeout form anywhere on the page.
    expect(screen.queryByLabelText("Max active runtimes")).toBeNull();
    expect(screen.queryByLabelText("Runtime idle timeout in seconds")).toBeNull();
    expect(screen.queryByRole("heading", { name: "Runtime pool" })).toBeNull();

    // fetch is only ever called for /system - no /system/settings round-trip.
    for (const call of (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls) {
      expect(String(call[0])).not.toContain("/system/settings");
    }
  });
});
