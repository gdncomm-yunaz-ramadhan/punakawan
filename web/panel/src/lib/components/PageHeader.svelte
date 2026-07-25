<script lang="ts">
  import type { Snippet } from "svelte";

  interface Props {
    title: string;
    description?: string;
    actions?: Snippet;
  }
  let { title, description, actions }: Props = $props();
</script>

<header class="page-header">
  <div class="text">
    <span class="eyebrow">Punakawan command center</span>
    <h1>{title}</h1>
    {#if description}
      <p class="description">{description}</p>
    {/if}
  </div>
  {#if actions}
    <div class="actions">
      {@render actions()}
    </div>
  {/if}
</header>

<style>
  .page-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 1rem;
    flex-wrap: wrap;
    margin-bottom: 1.35rem;
    padding-bottom: 1rem;
    border-bottom: 1px solid color-mix(in srgb, var(--color-border) 78%, transparent);
  }
  .text {
    min-width: 0;
  }
  .eyebrow {
    display: block;
    margin-bottom: 0.15rem;
    color: var(--color-accent);
    font-size: 0.69rem;
    font-weight: 750;
    letter-spacing: 0.09em;
    text-transform: uppercase;
  }
  h1 {
    font-size: clamp(1.45rem, 2vw, 1.9rem);
    line-height: 1.2;
    letter-spacing: -0.025em;
    margin: 0;
    color: var(--color-text);
  }
  .description {
    color: var(--color-text-muted);
    font-size: 0.9rem;
    max-width: 74ch;
    margin: 0.3rem 0 0;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
</style>
