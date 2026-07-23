import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import MobileNavigation from "../src/lib/components/MobileNavigation.svelte";
import { navigate } from "../src/lib/router/router.svelte";

describe("MobileNavigation", () => {
  it("renders a tab for each top-level route", () => {
    render(MobileNavigation);

    expect(screen.getByRole("link", { name: /Overview/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: /Workspaces/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: /Search/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: /System/ })).toBeTruthy();
  });

  it("marks the tab matching the current route as active and navigates on click", async () => {
    navigate("/");
    render(MobileNavigation);

    const workspacesTab = screen.getByRole("link", { name: /Workspaces/ });
    await fireEvent.click(workspacesTab);

    expect(window.location.pathname).toBe("/workspaces");
  });
});
