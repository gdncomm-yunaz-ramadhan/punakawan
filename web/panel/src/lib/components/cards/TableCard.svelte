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
    viewAllAction?: Snippet;
    children?: Snippet;
  }
  let { title, description, size = "wide", state = "default", emptyMessage, viewAllAction, children }: Props = $props();
</script>

<!--
  Card shell intended to host a DataTable (UI-011/§13.7) with a title
  and an optional "view all" action, e.g. a link to the full table page.
-->
<BentoCard {size} {state} {emptyMessage}>
  {#snippet header()}
    <div class="table-head">
      <div class="text">
        <h3>{title}</h3>
        {#if description}<p class="description">{description}</p>{/if}
      </div>
      {#if viewAllAction}
        <div class="action">{@render viewAllAction()}</div>
      {/if}
    </div>
  {/snippet}
  {#snippet children()}
    {#if children}
      {@render children()}
    {/if}
  {/snippet}
</BentoCard>

<style>
  .table-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.5rem;
    width: 100%;
  }
  .table-head h3 {
    margin: 0;
    font-size: 0.95rem;
    color: var(--color-text);
  }
  .description {
    margin: 0.15rem 0 0;
    font-size: 0.8rem;
    color: var(--color-text-muted);
  }
</style>
