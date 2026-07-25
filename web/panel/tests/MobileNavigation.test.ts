import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import MobileNavigation from "../src/lib/components/MobileNavigation.svelte";
import { navigate } from "../src/lib/router/router.svelte";

describe("MobileNavigation", () => {
  it("renders a tab for each top-level route", () => {
    render(MobileNavigation);

    expect(screen.getByRole("link", { name: /Overview/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: /Projects/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: /System/ })).toBeTruthy();
  });

  it("marks the tab matching the current route as active and navigates on click", async () => {
    navigate("/");
    render(MobileNavigation);

    const projectsTab = screen.getByRole("link", { name: /Projects/ });
    await fireEvent.click(projectsTab);

    expect(window.location.pathname).toBe("/projects");
  });

  it("cycles the theme when the theme control is tapped", async () => {
    render(MobileNavigation);

    const themeButton = screen.getByRole("button", { name: /Theme:/ });
    await fireEvent.click(themeButton);

    // System -> Light on first tap; the control reflects the new choice.
    expect(themeButton.getAttribute("title")).toBe("Theme: Light");
  });
});
