<script lang="ts">
  import BentoCard, { type BentoSize } from "./BentoCard.svelte";
  import type { CardState } from "./Card.svelte";

  interface Props {
    added: number;
    removed: number;
    modified: number;
    size?: BentoSize;
    state?: CardState;
    emptyMessage?: string;
  }
  let { added, removed, modified, size = "small", state = "default", emptyMessage }: Props = $props();
</script>

<!--
  Added/removed/modified counts as three numbers with icons/colors
  (UI-010): green +N, red -N, amber ~N.
-->
<BentoCard {size} {state} {emptyMessage}>
  {#snippet children()}
    <div class="diff-summary">
      <div class="metric metric-added">
        <span class="icon" aria-hidden="true">+</span>
        <span class="count">{added}</span>
        <span class="label">Added</span>
      </div>
      <div class="metric metric-removed">
        <span class="icon" aria-hidden="true">−</span>
        <span class="count">{removed}</span>
        <span class="label">Removed</span>
      </div>
      <div class="metric metric-modified">
        <span class="icon" aria-hidden="true">~</span>
        <span class="count">{modified}</span>
        <span class="label">Modified</span>
      </div>
    </div>
  {/snippet}
</BentoCard>

<style>
  .diff-summary {
    display: flex;
    justify-content: space-between;
    gap: 0.5rem;
    height: 100%;
    align-items: center;
  }
  .metric {
    display: grid;
    justify-items: center;
    gap: 0.1rem;
    flex: 1;
  }
  .icon {
    font-weight: 700;
    font-size: 1rem;
  }
  .count {
    font-weight: 700;
    font-size: 1.15rem;
  }
  .label {
    font-size: 0.75rem;
    color: var(--color-text-muted);
  }
  .metric-added .icon,
  .metric-added .count {
    color: var(--color-success);
  }
  .metric-removed .icon,
  .metric-removed .count {
    color: var(--color-danger);
  }
  .metric-modified .icon,
  .metric-modified .count {
    color: var(--color-warning);
  }
</style>
