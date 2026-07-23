// Shared typed shapes for chart components. Callers pass this simple
// labels/series data, never a raw Chart.js config - ChartShell is the only
// thing that knows about Chart.js's config shape, which keeps every typed
// chart component theme-aware and reduced-motion-safe "for free."
export type ChartKind = "line" | "bar";

export interface ChartSeries {
  /** Series name, shown in the legend and in the accessible table's header row. */
  name: string;
  values: number[];
  /** One of the semantic theme color roles (§13.2); resolved to a real color at render time. */
  colorRole?: "accent" | "secondary" | "success" | "warning" | "danger" | "info";
  /** Render this series stacked on top of others (bar charts only). */
  stack?: string;
}

export interface ChartData {
  kind: ChartKind;
  labels: string[];
  series: ChartSeries[];
  /** Optional axis titles, used both on the chart and as table column context. */
  xLabel?: string;
  yLabel?: string;
  /** Horizontal bar orientation (Chart.js "indexAxis: y"). Ignored for line charts. */
  horizontal?: boolean;
}
