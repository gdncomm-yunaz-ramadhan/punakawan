import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import DurationChart, { type DurationBucket } from "../src/lib/components/charts/DurationChart.svelte";

let constructedCount = 0;

vi.mock("chart.js", () => {
  class FakeChart {
    data: unknown;
    options: unknown;
    constructor(_canvas: unknown, config: { data: unknown; options: unknown }) {
      constructedCount++;
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
});

const buckets: DurationBucket[] = [
  { bucket: "< 1h", count: 10 },
  { bucket: "1-4h", count: 6 },
  { bucket: "4-24h", count: 3 },
  { bucket: "> 1d", count: 1 },
];

describe("DurationChart", () => {
  it("renders without crashing given sample buckets", async () => {
    render(DurationChart, { props: { buckets } });
    await waitFor(() => expect(constructedCount).toBe(1));
    expect(screen.getByLabelText("Cycle duration distribution")).toBeTruthy();
  });

  it("accessible fallback table has one row per bucket", async () => {
    render(DurationChart, { props: { buckets } });
    await waitFor(() => expect(constructedCount).toBe(1));

    const table = screen.getByText(/Cycle duration distribution:/).closest("table");
    expect(table?.querySelectorAll("tbody tr").length).toBe(4);
  });
});
