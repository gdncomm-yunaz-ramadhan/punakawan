import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import AppShellHarness from "./fixtures/AppShellHarness.svelte";

describe("AppShell", () => {
  it("renders the sidebar, top bar, and page content together", () => {
    render(AppShellHarness, { props: { system: null } });

    // Both Sidebar and MobileNavigation render a nav labeled "Primary" -
    // AppShell renders both simultaneously and lets CSS media queries
    // decide which is visible at a given viewport width.
    expect(screen.getAllByRole("navigation", { name: "Primary" })).toHaveLength(2);
    expect(screen.getByRole("banner")).toBeTruthy();
    expect(screen.getByTestId("shell-content").textContent).toBe("Page content");
  });

  it("passes the system prop through to TopBar", () => {
    render(AppShellHarness, {
      props: {
        system: {
          panel_version: "0.1.0",
          punakawan_version: "0.1.0",
          server_start_time: "2026-07-23T00:00:00Z",
          read_only: true,
          bound_address: "127.0.0.1:7331",
          registered_workspaces: 1,
          watcher_status: "not_implemented",
          feature_flags: [],
        },
      },
    });

    expect(screen.getByTestId("read-only-badge").textContent?.trim()).toBe("Read-only");
  });
});
