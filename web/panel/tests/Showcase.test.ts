import { fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Showcase from "../src/routes/showcase/Showcase.svelte";

// Showcase mounts every chart/graph component with sample data (Phase 2).
// jsdom has no real <canvas> context or WebGL, so both libraries are
// mocked here purely to keep this file's output clean - individual
// chart/graph components each have their own dedicated, more thorough
// mocked tests elsewhere in this directory.
vi.mock("chart.js", () => {
  class FakeChart {
    constructor(..._args: unknown[]) {}
    update = vi.fn();
    destroy = vi.fn();
    static register = vi.fn();
  }
  return {
    Chart: FakeChart,
    BarController: class {},
    BarElement: class {},
    CategoryScale: class {},
    Legend: class {},
    LinearScale: class {},
    LineController: class {},
    LineElement: class {},
    LogarithmicScale: class {},
    PointElement: class {},
    Tooltip: class {},
  };
});

vi.mock("cytoscape", () => {
  const factory = vi.fn((..._args: unknown[]) => ({
    on: vi.fn(),
    destroy: vi.fn(),
    style: () => ({ update: vi.fn() }),
    zoom: vi.fn(() => 1),
    fit: vi.fn(),
    layout: () => ({ run: vi.fn() }),
  }));
  return { default: factory };
});

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
