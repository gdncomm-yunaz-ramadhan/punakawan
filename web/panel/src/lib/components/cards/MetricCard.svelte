<script lang="ts">
  import BentoCard, { type BentoSize } from "./BentoCard.svelte";
  import type { CardState } from "./Card.svelte";

  export type TrendDirection = "up" | "down";
  // Optional batik accent tint for the metric. "none" (default) keeps the
  // original plain look so existing call sites are visually unchanged; any
  // other value paints a 3px colored left edge and a small emphasis chip.
  export type MetricAccent = "none" | "gold" | "teal" | "terracotta" | "indigo" | "danger" | "success";

  interface Props {
    label: string;
    value: string | number;
    trendDirection?: TrendDirection;
    trendDelta?: string;
    size?: BentoSize;
    state?: CardState;
    emptyMessage?: string;
    accent?: MetricAccent;
    /** Optional glyph shown in the emphasis chip when accent is set. */
    icon?: string;
  }
  let {
    label,
    value,
    trendDirection,
    trendDelta,
    size = "small",
    state = "default",
    emptyMessage,
    accent = "none",
    icon,
  }: Props = $props();

  // Maps each accent to the semantic/batik token it tints with. Kept as
  // token var() references so light/dark both resolve correctly.
  const accentColor: Record<MetricAccent, string> = {
    none: "transparent",
    gold: "var(--color-gold)",
    teal: "var(--color-teal)",
    terracotta: "var(--color-terracotta)",
    indigo: "var(--color-indigo)",
    danger: "var(--color-danger)",
    success: "var(--color-success)",
  };
</script>

<!--
  Big number + label + optional trend indicator, built on BentoCard
  (UI-010). Used for the overview's "Active revisions" / "Reviews
  needing input" / etc. style metrics (§13.5 row 1).
-->
<BentoCard {size} {state} {emptyMessage}>
  {#snippet children()}
    <div
      class="metric"
      class:accented={accent !== "none"}
      style:--metric-accent={accentColor[accent]}
    >
      {#if accent !== "none" && icon}
        <span class="chip" aria-hidden="true">{icon}</span>
      {/if}
      <span class="value">{value}</span>
      <span class="label">{label}</span>
      {#if trendDirection && trendDelta}
        <span class="trend trend-{trendDirection}">
          <span aria-hidden="true">{trendDirection === "up" ? "▲" : "▼"}</span>
          {trendDelta}
        </span>
      {/if}
    </div>
  {/snippet}
</BentoCard>

<style>
  .metric {
    position: relative;
    display: grid;
    gap: 0.2rem;
    align-content: center;
    height: 100%;
  }
  /* Colored left edge + inset padding when an accent is set. The plain
     (accent="none") metric is untouched. */
  .metric.accented {
    padding-left: 0.85rem;
  }
  .metric.accented::before {
    content: "";
    position: absolute;
    left: 0;
    top: 0.1rem;
    bottom: 0.1rem;
    width: 3px;
    border-radius: 999px;
    background: var(--metric-accent);
  }
  .chip {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 1.7rem;
    height: 1.7rem;
    margin-bottom: 0.15rem;
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--metric-accent) 16%, var(--color-surface));
    color: var(--metric-accent);
    font-size: 0.95rem;
    line-height: 1;
  }
  .value {
    font-size: 2.2rem;
    font-weight: 750;
    letter-spacing: -0.01em;
    color: var(--color-text);
    line-height: 1.05;
    font-variant-numeric: tabular-nums;
  }
  .label {
    color: var(--color-text-muted);
    font-size: 0.85rem;
    font-weight: 500;
  }
  .trend {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    font-size: 0.8rem;
    font-weight: 600;
    margin-top: 0.15rem;
  }
  .trend-up {
    color: var(--color-success);
  }
  .trend-down {
    color: var(--color-danger);
  }
</style>
