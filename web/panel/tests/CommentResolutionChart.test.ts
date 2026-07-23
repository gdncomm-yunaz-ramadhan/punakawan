import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import CommentResolutionChart, {
  type CommentResolutionSnapshot,
} from "../src/lib/components/charts/CommentResolutionChart.svelte";

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

const snapshots: CommentResolutionSnapshot[] = [
  { period: "Week 1", open: 4, addressed: 2, resolved: 6, wontfix: 1 },
  { period: "Week 2", open: 2, addressed: 3, resolved: 9, wontfix: 0 },
];

describe("CommentResolutionChart", () => {
  it("renders without crashing given sample snapshots", async () => {
    render(CommentResolutionChart, { props: { snapshots } });
    await waitFor(() => expect(constructedCount).toBe(1));
    expect(screen.getByLabelText("Comment resolution over time")).toBeTruthy();
  });

  it("stacks all four series on the same stack group", async () => {
    render(CommentResolutionChart, { props: { snapshots } });
    await waitFor(() => expect(constructedCount).toBe(1));

    const datasets = (lastConfig?.data as { datasets: { stack: string }[] }).datasets;
    expect(datasets).toHaveLength(4);
    expect(new Set(datasets.map((d) => d.stack)).size).toBe(1);
  });

  it("accessible fallback table has one row per period and one column per resolution state", async () => {
    render(CommentResolutionChart, { props: { snapshots } });
    await waitFor(() => expect(constructedCount).toBe(1));

    const table = screen.getByText(/Comment resolution over time:/).closest("table");
    expect(table?.querySelectorAll("tbody tr").length).toBe(2);
    expect(table?.querySelectorAll("thead th").length).toBe(5);
  });
});
