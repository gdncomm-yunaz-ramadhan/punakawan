import { fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import AccentPicker from "../src/lib/components/AccentPicker.svelte";
import { ACCENT_STORAGE_KEY } from "../src/lib/accent";

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
  document.documentElement.removeAttribute("style");
});

afterEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
  document.documentElement.removeAttribute("style");
});

describe("AccentPicker", () => {
  it("renders all five presets", () => {
    render(AccentPicker);
    for (const label of ["Indigo", "Teal", "Blue", "Violet", "Amber"]) {
      expect(screen.getByRole("radio", { name: label })).toBeTruthy();
    }
  });

  it("defaults to Indigo selected", () => {
    render(AccentPicker);
    expect(screen.getByRole("radio", { name: "Indigo" }).getAttribute("aria-checked")).toBe("true");
  });

  it("clicking Teal sets --color-accent to teal's light hex and persists the preset id", async () => {
    render(AccentPicker);

    await fireEvent.click(screen.getByRole("radio", { name: "Teal" }));

    expect(document.documentElement.style.getPropertyValue("--color-accent")).toBe("#0f9f9c");
    expect(localStorage.getItem(ACCENT_STORAGE_KEY)).toBe("teal");
    expect(screen.getByRole("radio", { name: "Teal" }).getAttribute("aria-checked")).toBe("true");
  });

  it("clicking a preset uses the dark hex pair when data-theme is dark", async () => {
    document.documentElement.setAttribute("data-theme", "dark");
    render(AccentPicker);

    await fireEvent.click(screen.getByRole("radio", { name: "Violet" }));

    expect(document.documentElement.style.getPropertyValue("--color-accent")).toBe("#a78bfa");
  });

  it("never touches warning, danger, or success tokens", async () => {
    document.documentElement.style.setProperty("--color-warning", "#b76e00");
    render(AccentPicker);

    await fireEvent.click(screen.getByRole("radio", { name: "Amber" }));

    expect(document.documentElement.style.getPropertyValue("--color-warning")).toBe("#b76e00");
    expect(document.documentElement.style.getPropertyValue("--color-danger")).toBe("");
    expect(document.documentElement.style.getPropertyValue("--color-success")).toBe("");
  });
});
