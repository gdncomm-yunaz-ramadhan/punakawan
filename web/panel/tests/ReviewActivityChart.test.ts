import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ReviewActivityChart, {
  type ActivityPoint,
} from "../src/lib/components/charts/ReviewActivityChart.svelte";

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

const points: ActivityPoint[] = [
  { period: "2026-07-01", reviewsCreated: 2, commentsAdded: 5, submissions: 1 },
  { period: "2026-07-02", reviewsCreated: 3, commentsAdded: 4, submissions: 2 },
];

describe("ReviewActivityChart", () => {
  it("renders without crashing given sample data", async () => {
    render(ReviewActivityChart, { props: { points } });
    await waitFor(() => expect(constructedCount).toBe(1));
    expect(screen.getByLabelText("Review activity over time")).toBeTruthy();
  });

  it("accessible fallback table has one row per period and one column per series", async () => {
    render(ReviewActivityChart, { props: { points } });
    await waitFor(() => expect(constructedCount).toBe(1));

    const table = screen.getByText(/Review activity over time:/).closest("table");
    expect(table?.querySelectorAll("tbody tr").length).toBe(2);
    // category column + 3 series columns (reviews created, comments, submissions)
    expect(table?.querySelectorAll("thead th").length).toBe(4);
  });
});
