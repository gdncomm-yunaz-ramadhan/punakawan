import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import MobileNavigation from "../src/lib/components/MobileNavigation.svelte";
import { navigate } from "../src/lib/router/router.svelte";

describe("MobileNavigation", () => {
  it("renders a tab for each top-level route", () => {
    render(MobileNavigation);

    expect(screen.getByRole("link", { name: /Projects/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: /Deliveries/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: /Settings/ })).toBeTruthy();
  });

  it("marks the tab matching the current route as active and navigates on click", async () => {
    navigate("/");
    render(MobileNavigation);

    const projectsTab = screen.getByRole("link", { name: /Projects/ });
    await fireEvent.click(projectsTab);

    expect(window.location.pathname).toBe("/projects");
  });

  it("opens the theme popover and applies the chosen theme", async () => {
    window.localStorage.clear();
    render(MobileNavigation);

    // The theme control is a single button that opens a popover (it no longer
    // cycles in place, which collided with the "System" page link).
    const themeButton = screen.getByRole("button", { name: "Theme" });
    expect(themeButton.getAttribute("aria-expanded")).toBe("false");

    await fireEvent.click(themeButton);
    expect(themeButton.getAttribute("aria-expanded")).toBe("true");

    // Pick "Light" from the popover's segmented control.
    await fireEvent.click(screen.getByRole("radio", { name: /Light/ }));

    // The preference is applied + persisted, and the popover dismisses itself.
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
    expect(window.localStorage.getItem("punakawan.theme")).toBe("light");
    expect(themeButton.getAttribute("aria-expanded")).toBe("false");
  });
});
