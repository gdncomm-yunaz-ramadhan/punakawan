import { fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import ThemeToggle from "../src/lib/components/ThemeToggle.svelte";
import { THEME_STORAGE_KEY } from "../src/lib/theme";

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
});

afterEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
});

describe("ThemeToggle", () => {
  it("renders all three options", () => {
    render(ThemeToggle);
    expect(screen.getByRole("radio", { name: "Light" })).toBeTruthy();
    expect(screen.getByRole("radio", { name: "Dark" })).toBeTruthy();
    expect(screen.getByRole("radio", { name: "System" })).toBeTruthy();
  });

  it("clicking Dark sets data-theme to dark and persists the preference", async () => {
    render(ThemeToggle);

    await fireEvent.click(screen.getByRole("radio", { name: "Dark" }));

    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    expect(screen.getByRole("radio", { name: "Dark" }).getAttribute("aria-checked")).toBe("true");
  });

  it("clicking Light sets data-theme to light and persists the preference", async () => {
    render(ThemeToggle);

    await fireEvent.click(screen.getByRole("radio", { name: "Light" }));

    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
  });

  it("clicking System resolves via prefers-color-scheme and persists 'system'", async () => {
    render(ThemeToggle);

    await fireEvent.click(screen.getByRole("radio", { name: "System" }));

    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("system");
    // jsdom's matchMedia (unmocked) reports no dark preference, so this
    // resolves to "light".
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });
});
