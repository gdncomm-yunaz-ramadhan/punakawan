import { fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import Showcase from "../src/routes/showcase/Showcase.svelte";

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

describe("Showcase", () => {
  it("renders without crashing and shows the page title", () => {
    render(Showcase);
    expect(screen.getByRole("heading", { name: "Component Showcase" })).toBeTruthy();
  });

  it("renders one instance of ThemeToggle and AccentPicker", () => {
    render(Showcase);
    expect(screen.getByRole("radiogroup", { name: "Theme" })).toBeTruthy();
    expect(screen.getByRole("radiogroup", { name: "Accent color" })).toBeTruthy();
  });

  it("renders the ResponsiveToolbar with a working overflow trigger", async () => {
    render(Showcase);
    expect(screen.getByTestId("toolbar-overflow-trigger")).toBeTruthy();
  });

  it("opens and closes the Drawer, Dialog, and BottomSheet triggers", async () => {
    render(Showcase);

    await fireEvent.click(screen.getByRole("button", { name: "Open Drawer" }));
    expect(screen.getByLabelText("Drawer example")).toBeTruthy();
    await fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByLabelText("Drawer example")).toBeNull();

    await fireEvent.click(screen.getByRole("button", { name: "Open Dialog" }));
    expect(screen.getByRole("dialog", { name: "Dialog example" })).toBeTruthy();
    await fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog", { name: "Dialog example" })).toBeNull();

    await fireEvent.click(screen.getByRole("button", { name: "Open BottomSheet" }));
    expect(screen.getByRole("dialog", { name: "Bottom sheet example" })).toBeTruthy();
    await fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog", { name: "Bottom sheet example" })).toBeNull();
  });

  it("renders the StickyActionBar's primary action", () => {
    render(Showcase);
    expect(screen.getByRole("button", { name: "Primary action" })).toBeTruthy();
  });
});
