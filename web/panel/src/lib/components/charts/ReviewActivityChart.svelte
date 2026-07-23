<script lang="ts">
  import type { ChartConfiguration } from "chart.js";
  import ChartShell from "./ChartShell.svelte";
  import { resolveSeriesColor } from "./colorRoles";
  import type { ThemePalette } from "./themeColors";
  import type { ChartData } from "./types";

  export interface ActivityPoint {
    /** e.g. an ISO date or week label. */
    period: string;
    reviewsCreated: number;
    commentsAdded: number;
    submissions: number;
  }

  interface Props {
    points: ActivityPoint[];
    title?: string;
  }
  let { points, title = "Review activity over time" }: Props = $props();

  const data = $derived<ChartData>({
    kind: "line",
    labels: points.map((p) => p.period),
    xLabel: "Period",
    yLabel: "Count",
    series: [
      { name: "Reviews created", values: points.map((p) => p.reviewsCreated), colorRole: "accent" },
      { name: "Comments added", values: points.map((p) => p.commentsAdded), colorRole: "info" },
      { name: "Submissions", values: points.map((p) => p.submissions), colorRole: "success" },
    ],
  });

  function buildConfig(chartData: ChartData, palette: ThemePalette, reducedMotion: boolean): ChartConfiguration {
    return {
      type: "line",
      data: {
        labels: chartData.labels,
        datasets: chartData.series.map((series, i) => {
          const color = resolveSeriesColor(series, i, palette);
          return {
            label: series.name,
            data: series.values,
            borderColor: color,
            backgroundColor: color,
            pointBackgroundColor: color,
            tension: 0.25,
            fill: false,
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
            title: { display: true, text: chartData.xLabel, color: palette.textMuted },
            ticks: { color: palette.textMuted },
            grid: { color: palette.border },
          },
          y: {
            title: { display: true, text: chartData.yLabel, color: palette.textMuted },
            ticks: { color: palette.textMuted },
            grid: { color: palette.border },
            beginAtZero: true,
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
  tableCaption={`${title}: reviews created, comments added, and submissions per period.`}
/>
