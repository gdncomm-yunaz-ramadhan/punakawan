<script lang="ts">
  import type { ChartConfiguration } from "chart.js";
  import ChartShell from "./ChartShell.svelte";
  import { resolveSeriesColor } from "./colorRoles";
  import type { ThemePalette } from "./themeColors";
  import type { ChartData } from "./types";

  export interface DurationBucket {
    /** e.g. "< 1h", "1-4h", "4-24h", "1-3d", "> 3d". */
    bucket: string;
    count: number;
  }

  interface Props {
    /** Distribution of review/revision cycle durations across buckets. */
    buckets: DurationBucket[];
    title?: string;
    /** Axis label for the count dimension, e.g. "Revisions" or "Reviews". */
    countLabel?: string;
  }
  let { buckets, title = "Cycle duration distribution", countLabel = "Count" }: Props = $props();

  const data = $derived<ChartData>({
    kind: "bar",
    labels: buckets.map((b) => b.bucket),
    xLabel: "Duration bucket",
    yLabel: countLabel,
    series: [{ name: countLabel, values: buckets.map((b) => b.count), colorRole: "accent" }],
  });

  function buildConfig(chartData: ChartData, palette: ThemePalette, reducedMotion: boolean): ChartConfiguration {
    return {
      type: "bar",
      data: {
        labels: chartData.labels,
        datasets: chartData.series.map((series, i) => {
          const color = resolveSeriesColor(series, i, palette);
          return { label: series.name, data: series.values, backgroundColor: color, borderColor: color };
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
            beginAtZero: true,
            title: { display: true, text: chartData.yLabel, color: palette.textMuted },
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
  tableCaption={`${title}: number of items falling into each duration bucket.`}
/>
