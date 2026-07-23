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
  let { title, description, size = "medium", state = "default", emptyMessage, children }: Props = $props();
</script>

<!--
  Card shell for whatever graph/connector visual eventually goes inside
  (another phase owns the Cytoscape.js GraphCanvas - this only provides
  the title/description header and a content slot, per §13.6/§13.9).
-->
<BentoCard {size} {state} {emptyMessage}>
  {#snippet header()}
    <div class="graph-head">
      <h3>{title}</h3>
      {#if description}<p class="description">{description}</p>{/if}
    </div>
  {/snippet}
  {#snippet children()}
    <div class="graph-body">
      {#if children}
        {@render children()}
      {:else}
        <p class="placeholder">No graph content provided.</p>
      {/if}
    </div>
  {/snippet}
</BentoCard>

<style>
  .graph-head h3 {
    margin: 0;
    font-size: 0.95rem;
    color: var(--color-text);
  }
  .description {
    margin: 0.15rem 0 0;
    font-size: 0.8rem;
    color: var(--color-text-muted);
  }
  .graph-body {
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
