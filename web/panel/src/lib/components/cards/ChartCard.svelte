<script lang="ts">
  import BentoCard, { type BentoSize } from "./BentoCard.svelte";
  import type { CardState } from "./Card.svelte";
  import type { Snippet } from "svelte";

  interface Props {
    title: string;
    description?: string;
    size?: BentoSize;
    state?: CardState;
    emptyMessage?: string;
    children?: Snippet;
  }
  let { title, description, size = "wide", state = "default", emptyMessage, children }: Props = $props();
</script>

<!--
  Card shell for whatever chart component eventually goes inside
  (another phase owns the Chart.js adapters - this only provides the
  title/description header and a content slot, per §13.6/§13.8).
-->
<BentoCard {size} {state} {emptyMessage}>
  {#snippet header()}
    <div class="chart-head">
      <h3>{title}</h3>
      {#if description}<p class="description">{description}</p>{/if}
    </div>
  {/snippet}
  {#snippet children()}
    <div class="chart-body">
      {#if children}
        {@render children()}
      {:else}
        <p class="placeholder">No chart content provided.</p>
      {/if}
    </div>
  {/snippet}
</BentoCard>

<style>
  .chart-head h3 {
    margin: 0;
    font-size: 0.95rem;
    color: var(--color-text);
  }
  .description {
    margin: 0.15rem 0 0;
    font-size: 0.8rem;
    color: var(--color-text-muted);
  }
  .chart-body {
    min-height: 160px;
    display: flex;
    align-items: stretch;
  }
  .placeholder {
    margin: auto;
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
</style>
