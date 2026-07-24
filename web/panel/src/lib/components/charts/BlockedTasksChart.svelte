<script lang="ts">
  import type { ChartConfiguration } from "chart.js";
  import ChartShell from "./ChartShell.svelte";
  import type { ThemePalette } from "./themeColors";
  import type { ChartData } from "./types";

  export interface BlockedTasksDatum {
    /** Workspace/project display label. */
    label: string;
    /** Number of blocked tasks in that workspace. */
    value: number;
  }

  interface Props {
    items: BlockedTasksDatum[];
    title?: string;
    /** Axis label for the count dimension. */
    countLabel?: string;
  }
  let { items, title = "Blocked tasks by workspace", countLabel = "Blocked tasks" }: Props = $props();

  const data = $derived<ChartData>({
    kind: "bar",
    labels: items.map((d) => d.label),
    xLabel: countLabel,
    yLabel: "Workspace",
    horizontal: true,
    series: [{ name: countLabel, values: items.map((d) => d.value) }],
  });

  // Categorical wayang-batik rotation, applied per BAR (not per series) so
  // each workspace reads as a distinct, on-brand hue. This deliberately
  // bypasses colorRoles' per-series resolver, which paints one color for a
  // whole dataset - here we want one color per category.
  const ROTATION = ["gold", "teal", "terracotta", "indigo", "violet"] as const;

  function buildConfig(chartData: ChartData, palette: ThemePalette, reducedMotion: boolean): ChartConfiguration {
    const barColors = chartData.labels.map((_, i) => palette[ROTATION[i % ROTATION.length]]);
    return {
      type: "bar",
      data: {
        labels: chartData.labels,
        datasets: [
          {
            label: countLabel,
            data: chartData.series[0]?.values ?? [],
            backgroundColor: barColors,
            borderColor: barColors,
            borderRadius: 4,
            maxBarThickness: 34,
          },
        ],
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
            // Blocked-task counts are whole numbers; drop fractional ticks.
            ticks: { color: palette.textMuted, precision: 0 },
            grid: { color: palette.border },
            title: { display: true, text: chartData.xLabel, color: palette.textMuted },
          },
          y: {
            ticks: { color: palette.text },
            grid: { display: false },
          },
        },
        plugins: {
          legend: { display: false },
          tooltip: {
            callbacks: {
              label: (ctx) => ` ${ctx.parsed.x} blocked`,
            },
          },
        },
      },
    };
  }
</script>

<ChartShell
  ariaLabel={title}
  {data}
  {buildConfig}
  height={Math.max(160, items.length * 46 + 48)}
  tableCaption={`${title}: number of blocked tasks per workspace.`}
/>
