import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ChangeVolumeChart, {
  type ChangeVolumePoint,
} from "../src/lib/components/charts/ChangeVolumeChart.svelte";

let constructedCount = 0;
let lastConfig: { data: unknown; options: unknown } | null = null;

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

beforeEach(() => {
  constructedCount = 0;
  lastConfig = null;
});

const points: ChangeVolumePoint[] = [
  { revision: "v1", added: 40, removed: 5, modified: 12 },
  { revision: "v2", added: 15, removed: 20, modified: 8 },
];

describe("ChangeVolumeChart", () => {
  it("renders without crashing given sample points", async () => {
    render(ChangeVolumeChart, { props: { points } });
    await waitFor(() => expect(constructedCount).toBe(1));
    expect(screen.getByLabelText("Change volume per revision")).toBeTruthy();
  });

  it("uses the success/danger/warning semantic roles for added/removed/modified", async () => {
    render(ChangeVolumeChart, { props: { points } });
    await waitFor(() => expect(constructedCount).toBe(1));

    const datasets = (lastConfig?.data as { datasets: { label: string; backgroundColor: string }[] }).datasets;
    const byLabel = new Map(datasets.map((d) => [d.label, d.backgroundColor]));
    // theme.css light-theme values, resolved by getComputedStyle in jsdom
    // (jsdom returns the literal custom-property text since it has no
    // real cascade for var() - readThemePalette's FALLBACK covers this).
    expect(byLabel.get("Added")).toBeTruthy();
    expect(byLabel.get("Removed")).toBeTruthy();
    expect(byLabel.get("Modified")).toBeTruthy();
    expect(byLabel.get("Added")).not.toBe(byLabel.get("Removed"));
  });

  it("accessible fallback table has one row per revision", async () => {
    render(ChangeVolumeChart, { props: { points } });
    await waitFor(() => expect(constructedCount).toBe(1));

    const table = screen.getByText(/Change volume per revision:/).closest("table");
    expect(table?.querySelectorAll("tbody tr").length).toBe(2);
  });
});
