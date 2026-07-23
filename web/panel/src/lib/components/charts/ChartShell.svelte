<script lang="ts">
  import { onDestroy, onMount, untrack } from "svelte";
  import type { Chart, ChartConfiguration } from "chart.js";
  import { loadChart } from "./chartLoader";
  import {
    prefersReducedMotion,
    readThemePalette,
    watchReducedMotionChange,
    watchThemeChange,
    type ThemePalette,
  } from "./themeColors";
  import type { ChartData } from "./types";

  interface Props {
    /** Concise label for the canvas itself; the full data table is the real accessible equivalent. */
    ariaLabel: string;
    data: ChartData;
    /** Builds the Chart.js config from typed data + the live theme palette. Owned by each typed chart component. */
    buildConfig: (data: ChartData, palette: ThemePalette, reducedMotion: boolean) => ChartConfiguration;
    /** Caption for the accessible fallback table. */
    tableCaption: string;
    /** Show the fallback table visibly instead of screen-reader-only. Defaults to hidden. */
    showTable?: boolean;
    height?: number;
  }
  let { ariaLabel, data, buildConfig, tableCaption, showTable = false, height = 260 }: Props = $props();

  let canvasEl: HTMLCanvasElement | undefined = $state();
  let chartInstance: Chart | null = null;
  let loading = $state(true);
  let loadError: string | null = $state(null);
  // Seeded once from the prop's initial value, not kept in sync with it -
  // this is a default for the user's own toggle button below, not a
  // controlled prop, so re-reading `showTable` reactively would stomp on
  // a manual toggle click if the caller's prop value happens to change.
  let tableVisible = $state(untrack(() => showTable));

  let unwatchTheme: (() => void) | null = null;
  let unwatchMotion: (() => void) | null = null;

  async function render() {
    try {
      const ChartCtor = await loadChart();
      loading = false;
      if (!canvasEl) return;
      const palette = readThemePalette();
      const reduced = prefersReducedMotion();
      const config = buildConfig(data, palette, reduced);
      if (chartInstance) {
        chartInstance.destroy();
        chartInstance = null;
      }
      chartInstance = new ChartCtor(canvasEl, config);
    } catch (e) {
      loading = false;
      loadError = e instanceof Error ? e.message : String(e);
    }
  }

  function recolor() {
    if (!chartInstance || !canvasEl) return;
    const palette = readThemePalette();
    const reduced = prefersReducedMotion();
    const config = buildConfig(data, palette, reduced);
    chartInstance.data = config.data;
    chartInstance.options = config.options ?? {};
    chartInstance.update();
  }

  onMount(() => {
    render();
    unwatchTheme = watchThemeChange(recolor);
    unwatchMotion = watchReducedMotionChange(() => recolor());
  });

  onDestroy(() => {
    unwatchTheme?.();
    unwatchMotion?.();
    chartInstance?.destroy();
    chartInstance = null;
  });

  // Re-render (not just recolor) when the caller swaps in new data.
  $effect(() => {
    data;
    if (!loading) render();
  });
</script>

<div class="chart-shell" style={`--chart-height: ${height}px`}>
  {#if loading}
    <div class="skeleton" role="status" aria-live="polite">
      <span class="sr-only">Loading chart…</span>
    </div>
  {:else if loadError}
    <p role="alert" class="chart-error">Chart failed to load: {loadError}</p>
  {:else}
    <canvas bind:this={canvasEl} aria-label={ariaLabel} class="chart-canvas"></canvas>
  {/if}
  {#if canvasEl || loading}
    <button type="button" class="table-toggle" onclick={() => (tableVisible = !tableVisible)}>
      {tableVisible ? "Hide data table" : "View as data table"}
    </button>
  {/if}

  <table class:sr-only={!tableVisible}>
    <caption>{tableCaption}</caption>
    <thead>
      <tr>
        <th scope="col">{data.xLabel ?? "Category"}</th>
        {#each data.series as series (series.name)}
          <th scope="col">{series.name}</th>
        {/each}
      </tr>
    </thead>
    <tbody>
      {#each data.labels as label, i (label + i)}
        <tr>
          <th scope="row">{label}</th>
          {#each data.series as series (series.name)}
            <td>{series.values[i] ?? ""}</td>
          {/each}
        </tr>
      {/each}
    </tbody>
  </table>
</div>

<style>
  .chart-shell {
    display: grid;
    gap: 0.5rem;
  }
  .chart-canvas {
    width: 100%;
    height: var(--chart-height);
    max-height: var(--chart-height);
  }
  .skeleton {
    width: 100%;
    height: var(--chart-height);
    border-radius: var(--radius-card);
    background: linear-gradient(
      90deg,
      var(--color-surface-subtle) 25%,
      var(--color-surface-raised) 37%,
      var(--color-surface-subtle) 63%
    );
    background-size: 400% 100%;
  }
  @media (prefers-reduced-motion: no-preference) {
    .skeleton {
      animation: shimmer 1.4s ease infinite;
    }
  }
  @keyframes shimmer {
    0% {
      background-position: 100% 50%;
    }
    100% {
      background-position: 0 50%;
    }
  }
  .chart-error {
    color: var(--color-danger);
    font-size: 0.85rem;
  }
  .table-toggle {
    justify-self: start;
    font-size: 0.78rem;
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    color: var(--color-text-muted);
    border-radius: 6px;
    padding: 0.25rem 0.6rem;
    cursor: pointer;
    min-height: 32px;
  }
  table {
    border-collapse: collapse;
    font-size: 0.8rem;
    color: var(--color-text);
  }
  table:not(.sr-only) {
    margin-top: 0.25rem;
  }
  th,
  td {
    border: 1px solid var(--color-border);
    padding: 0.3rem 0.5rem;
    text-align: right;
  }
  thead th,
  tbody th[scope="row"] {
    text-align: left;
    background: var(--color-surface-subtle);
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
</style>
