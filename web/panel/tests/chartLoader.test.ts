import { beforeEach, describe, expect, it, vi } from "vitest";

const registerSpy = vi.fn();

vi.mock("chart.js", () => {
  class FakeChart {
    static register = registerSpy;
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
  registerSpy.mockClear();
});

describe("chartLoader", () => {
  it("registers Chart.js controllers/elements/scales exactly once even across repeated loadChart calls", async () => {
    const { loadChart, __resetChartLoaderForTests } = await import("../src/lib/components/charts/chartLoader");
    __resetChartLoaderForTests();

    const first = await loadChart();
    const second = await loadChart();

    expect(first).toBe(second);
    expect(registerSpy).toHaveBeenCalledTimes(1);
  });

  it("caches the in-flight promise so concurrent callers share one import", async () => {
    const { loadChart, __resetChartLoaderForTests } = await import("../src/lib/components/charts/chartLoader");
    __resetChartLoaderForTests();

    const [a, b] = await Promise.all([loadChart(), loadChart()]);
    expect(a).toBe(b);
    expect(registerSpy).toHaveBeenCalledTimes(1);
  });
});
