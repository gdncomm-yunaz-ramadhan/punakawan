<script lang="ts">
  import type { ChartConfiguration } from "chart.js";
  import ChartShell from "./ChartShell.svelte";
  import { resolveSeriesColor } from "./colorRoles";
  import type { ThemePalette } from "./themeColors";
  import type { ChartData } from "./types";

  export interface CommentResolutionSnapshot {
    /** e.g. an ISO date or week label. */
    period: string;
    open: number;
    addressed: number;
    resolved: number;
    wontfix: number;
  }

  interface Props {
    snapshots: CommentResolutionSnapshot[];
    title?: string;
  }
  let { snapshots, title = "Comment resolution over time" }: Props = $props();

  const STACK = "resolution";

  const data = $derived<ChartData>({
    kind: "bar",
    labels: snapshots.map((s) => s.period),
    xLabel: "Period",
    yLabel: "Comments",
    series: [
      { name: "Open", values: snapshots.map((s) => s.open), colorRole: "info", stack: STACK },
      { name: "Addressed", values: snapshots.map((s) => s.addressed), colorRole: "warning", stack: STACK },
      { name: "Resolved", values: snapshots.map((s) => s.resolved), colorRole: "success", stack: STACK },
      { name: "Won't fix", values: snapshots.map((s) => s.wontfix), colorRole: "danger", stack: STACK },
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
  tableCaption={`${title}: open, addressed, resolved, and won't-fix comment counts per period.`}
/>
