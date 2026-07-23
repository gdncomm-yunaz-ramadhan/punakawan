<script lang="ts">
  import type { ChartConfiguration } from "chart.js";
  import ChartShell from "./ChartShell.svelte";
  import { resolveSeriesColor } from "./colorRoles";
  import type { ThemePalette } from "./themeColors";
  import type { ChartData } from "./types";

  export interface ChangeVolumePoint {
    /** e.g. "Revision 1", "v3", or a date label. */
    revision: string;
    added: number;
    removed: number;
    modified: number;
  }

  interface Props {
    points: ChangeVolumePoint[];
    title?: string;
  }
  let { points, title = "Change volume per revision" }: Props = $props();

  const STACK = "volume";

  // Reuses the same green/red/amber semantic convention as DiffSummaryCard
  // elsewhere in the app: added -> success, removed -> danger,
  // modified -> warning.
  const data = $derived<ChartData>({
    kind: "bar",
    labels: points.map((p) => p.revision),
    xLabel: "Revision",
    yLabel: "Lines",
    series: [
      { name: "Added", values: points.map((p) => p.added), colorRole: "success", stack: STACK },
      { name: "Removed", values: points.map((p) => p.removed), colorRole: "danger", stack: STACK },
      { name: "Modified", values: points.map((p) => p.modified), colorRole: "warning", stack: STACK },
    ],
  });

  function buildConfig(chartData: ChartData, palette: ThemePalette, reducedMotion: boolean): ChartConfiguration {
    return {
      type: "bar",
      data: {
        labels: chartData.labels,
        datasets: chartData.series.map((series, i) => {
          const color = resolveSeriesColor(series, i, palette);
          return {
            label: series.name,
            data: series.values,
            backgroundColor: color,
            borderColor: color,
            stack: series.stack,
          };
        }),
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: reducedMotion ? false : undefined,
        color: palette.text,
        scales: {
          x: {
            stacked: true,
            title: { display: true, text: chartData.xLabel, color: palette.textMuted },
            ticks: { color: palette.textMuted },
            grid: { color: palette.border },
          },
          y: {
            stacked: true,
            beginAtZero: true,
            title: { display: true, text: chartData.yLabel, color: palette.textMuted },
            ticks: { color: palette.textMuted },
            grid: { color: palette.border },
          },
        },
        plugins: {
          legend: { labels: { color: palette.text } },
        },
      },
    };
  }
</script>

<ChartShell
  ariaLabel={title}
  {data}
  {buildConfig}
  tableCaption={`${title}: added, removed, and modified line counts per revision.`}
/>
