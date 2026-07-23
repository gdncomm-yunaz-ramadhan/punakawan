<script lang="ts">
  import type { ChartConfiguration } from "chart.js";
  import ChartShell from "./ChartShell.svelte";
  import type { ThemePalette } from "./themeColors";
  import type { ChartData } from "./types";

  export interface RevisionOutcomeCounts {
    accepted: number;
    rejected: number;
    superseded: number;
  }

  interface Props {
    counts: RevisionOutcomeCounts;
    title?: string;
  }
  let { counts, title = "Revision outcomes" }: Props = $props();

  // Horizontal bar with 3-5 categories reads better than a donut and is
  // straightforward to express in the accessible fallback table (one row
  // per category rather than needing angle/arc description).
  const data = $derived<ChartData>({
    kind: "bar",
    horizontal: true,
    labels: ["Accepted", "Rejected", "Superseded"],
    xLabel: "Outcome",
    yLabel: "Count",
    series: [
      {
        name: "Revisions",
        values: [counts.accepted, counts.rejected, counts.superseded],
      },
    ],
  });

  const CATEGORY_ROLES: (keyof ThemePalette)[] = ["success", "danger", "warning"];

  function buildConfig(chartData: ChartData, palette: ThemePalette, reducedMotion: boolean): ChartConfiguration {
    const colors = CATEGORY_ROLES.map((role) => palette[role]);
    return {
      type: "bar",
      data: {
        labels: chartData.labels,
        datasets: chartData.series.map((series) => ({
          label: series.name,
          data: series.values,
          backgroundColor: colors,
          borderColor: colors,
        })),
      },
      options: {
        indexAxis: "y",
        responsive: true,
        maintainAspectRatio: false,
        animation: reducedMotion ? false : undefined,
        color: palette.text,
        scales: {
          x: {
            beginAtZero: true,
            ticks: { color: palette.textMuted },
            grid: { color: palette.border },
          },
          y: {
            ticks: { color: palette.textMuted },
            grid: { color: palette.border },
          },
        },
        plugins: {
          legend: { display: false },
        },
      },
    };
  }
</script>

<ChartShell
  ariaLabel={title}
  {data}
  {buildConfig}
  tableCaption={`${title}: count of revisions by outcome.`}
/>
