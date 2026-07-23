// Lazy singleton loader for Chart.js (§ Artifact Review Phase 2).
//
// Chart.js must never be part of the base app bundle - it is only pulled
// in the moment a chart is actually about to render. We use the modular
// tree-shaken API (registering only the specific controllers/elements/
// scales/plugins the five chart components below need) rather than the
// `chart.js/auto` convenience import, which registers everything Chart.js
// ships and is significantly larger - the CI budget here is <120KB
// compressed for the chart chunk.
//
// Every symbol pulled from "chart.js" - including the controller/element/
// scale classes needed for Chart.register() - must come from the single
// dynamic `import("chart.js")` below. A *static* top-level `import { ... }
// from "chart.js"` anywhere in this module (even just for types re-used at
// runtime) makes Vite treat the module as eagerly reachable and bundles
// it into the base chunk regardless of this function being lazy - see
// the build warning that catches this if it regresses.
import type { Chart as ChartType } from "chart.js";

let registered = false;
let loadPromise: Promise<typeof ChartType> | null = null;

// Returns the Chart.js constructor, dynamically importing the module on
// first call and caching the in-flight/resolved promise for subsequent
// callers (so mounting five chart components on one page only pays the
// import cost once).
export async function loadChart(): Promise<typeof ChartType> {
  if (!loadPromise) {
    loadPromise = import("chart.js").then((mod) => {
      if (!registered) {
        mod.Chart.register(
          mod.BarController,
          mod.BarElement,
          mod.LineController,
          mod.LineElement,
          mod.PointElement,
          mod.CategoryScale,
          mod.LinearScale,
          mod.LogarithmicScale,
          mod.Legend,
          mod.Tooltip,
        );
        registered = true;
      }
      return mod.Chart;
    });
  }
  return loadPromise;
}

// Test-only hook: resets the cached loader so each test file can mock
// "chart.js" freshly without a prior test's real import lingering.
export function __resetChartLoaderForTests(): void {
  loadPromise = null;
  registered = false;
}
