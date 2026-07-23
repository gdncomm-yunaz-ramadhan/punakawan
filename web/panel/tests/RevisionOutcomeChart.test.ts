import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import RevisionOutcomeChart from "../src/lib/components/charts/RevisionOutcomeChart.svelte";

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

describe("RevisionOutcomeChart", () => {
  it("renders without crashing given sample counts", async () => {
    render(RevisionOutcomeChart, { props: { counts: { accepted: 8, rejected: 2, superseded: 1 } } });
    await waitFor(() => expect(constructedCount).toBe(1));
    expect(screen.getByLabelText("Revision outcomes")).toBeTruthy();
  });

  it("renders as a horizontal bar chart (indexAxis: y)", async () => {
    render(RevisionOutcomeChart, { props: { counts: { accepted: 8, rejected: 2, superseded: 1 } } });
    await waitFor(() => expect(constructedCount).toBe(1));
    expect((lastConfig?.options as { indexAxis?: string })?.indexAxis).toBe("y");
  });

  it("accessible fallback table has exactly 3 rows (accepted/rejected/superseded)", async () => {
    render(RevisionOutcomeChart, { props: { counts: { accepted: 8, rejected: 2, superseded: 1 } } });
    await waitFor(() => expect(constructedCount).toBe(1));

    const table = screen.getByText(/Revision outcomes:/).closest("table");
    expect(table?.querySelectorAll("tbody tr").length).toBe(3);
  });
});
