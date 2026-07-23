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
    margin-bottom: 1rem;
  }
  h1 {
    font-size: 1.25rem;
    margin: 0;
    color: var(--color-text);
  }
  .description {
    color: var(--color-text-muted);
    font-size: 0.85rem;
    margin: 0.2rem 0 0;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
</style>
