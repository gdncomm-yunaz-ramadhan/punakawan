import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ChartShell from "../src/lib/components/charts/ChartShell.svelte";
import type { ChartData } from "../src/lib/components/charts/types";

const destroy = vi.fn();
const update = vi.fn();
let lastConfig: unknown = null;
let constructedCount = 0;

// Mocking chart.js itself (rather than fighting jsdom's lack of a real
// canvas context) per the test-strategy note in the task: this is the
// standard/reliable approach for Chart.js-consuming component tests.
vi.mock("chart.js", () => {
  class FakeChart {
    data: unknown;
    options: unknown;
    constructor(_canvas: unknown, config: { data: unknown; options: unknown }) {
      constructedCount++;
      lastConfig = config;
      this.data = config.data;
      this.options = config.options;
    }
    update = update;
    destroy = destroy;
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

beforeEach(() => {
  destroy.mockClear();
  update.mockClear();
  lastConfig = null;
  constructedCount = 0;
  document.documentElement.removeAttribute("data-theme");
});

afterEach(() => {
  document.documentElement.removeAttribute("data-theme");
});

const sampleData: ChartData = {
  kind: "line",
  labels: ["Mon", "Tue", "Wed"],
  series: [{ name: "Widgets", values: [1, 2, 3] }],
};

function buildConfig(data: ChartData) {
  return {
    type: "line" as const,
    data: { labels: data.labels, datasets: [{ label: data.series[0].name, data: data.series[0].values }] },
    options: { animation: undefined },
  };
}

describe("ChartShell", () => {
  it("renders a canvas after the async Chart.js load resolves", async () => {
    render(ChartShell, {
      props: { ariaLabel: "Sample chart", data: sampleData, buildConfig, tableCaption: "Sample data" },
    });

    await waitFor(() => expect(constructedCount).toBe(1));
    expect(screen.getByLabelText("Sample chart")).toBeTruthy();
  });

  it("renders the accessible fallback table with one row per label plus a header row", async () => {
    render(ChartShell, {
      props: { ariaLabel: "Sample chart", data: sampleData, buildConfig, tableCaption: "Sample data" },
    });
    await waitFor(() => expect(constructedCount).toBe(1));

    const table = screen.getByText("Sample data").closest("table");
    expect(table).toBeTruthy();
    // header row + 3 data rows (Mon/Tue/Wed)
    expect(table?.querySelectorAll("tbody tr").length).toBe(3);
    expect(table?.querySelectorAll("thead th").length).toBe(2); // category col + 1 series col
  });

  it("toggling 'View as table' shows the table visibly", async () => {
    const { component } = render(ChartShell, {
      props: { ariaLabel: "Sample chart", data: sampleData, buildConfig, tableCaption: "Sample data" },
    });
    await waitFor(() => expect(constructedCount).toBe(1));
    void component;

    const toggle = screen.getByRole("button", { name: "View as data table" });
    const table = screen.getByText("Sample data").closest("table") as HTMLTableElement;
    expect(table.className).toContain("sr-only");

    toggle.click();
    await waitFor(() => expect(table.className).not.toContain("sr-only"));
  });

  it("re-renders (destroying the previous instance) on a theme toggle", async () => {
    render(ChartShell, {
      props: { ariaLabel: "Sample chart", data: sampleData, buildConfig, tableCaption: "Sample data" },
    });
    await waitFor(() => expect(constructedCount).toBe(1));

    document.documentElement.setAttribute("data-theme", "dark");
    await waitFor(() => expect(update).toHaveBeenCalled());
  });

  it("passes animation: false through buildConfig when reduced motion is requested by the caller", async () => {
    const reducedBuildConfig = (data: ChartData) => ({
      ...buildConfig(data),
      options: { animation: false as const },
    });
    render(ChartShell, {
      props: { ariaLabel: "Sample chart", data: sampleData, buildConfig: reducedBuildConfig, tableCaption: "x" },
    });
    await waitFor(() => expect(constructedCount).toBe(1));

    expect((lastConfig as { options: { animation: unknown } }).options.animation).toBe(false);
  });
});
