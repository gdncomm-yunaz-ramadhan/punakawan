<script lang="ts">
  import BentoCard, { type BentoSize } from "./BentoCard.svelte";
  import type { CardState } from "./Card.svelte";

  export type TrendDirection = "up" | "down";

  interface Props {
    label: string;
    value: string | number;
    trendDirection?: TrendDirection;
    trendDelta?: string;
    size?: BentoSize;
    state?: CardState;
    emptyMessage?: string;
  }
  let { label, value, trendDirection, trendDelta, size = "small", state = "default", emptyMessage }: Props = $props();
</script>

<!--
  Big number + label + optional trend indicator, built on BentoCard
  (UI-010). Used for the overview's "Active revisions" / "Reviews
  needing input" / etc. style metrics (§13.5 row 1).
-->
<BentoCard {size} {state} {emptyMessage}>
  {#snippet children()}
    <div class="metric">
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
    display: grid;
    gap: 0.2rem;
    align-content: center;
    height: 100%;
  }
  .value {
    font-size: 1.9rem;
    font-weight: 700;
    color: var(--color-text);
    line-height: 1.1;
  }
  .label {
    color: var(--color-text-muted);
    font-size: 0.85rem;
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
