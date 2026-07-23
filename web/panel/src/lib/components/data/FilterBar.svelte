<script lang="ts">
  import type { Snippet } from "svelte";

  interface Props {
    searchValue?: string;
    searchPlaceholder?: string;
    onSearchChange?: (value: string) => void;
    filters?: Snippet;
  }
  let {
    searchValue = $bindable(""),
    searchPlaceholder = "Search",
    onSearchChange,
    filters,
  }: Props = $props();

  function onInput(e: Event) {
    searchValue = (e.target as HTMLInputElement).value;
    onSearchChange?.(searchValue);
  }
</script>

<!--
  Horizontal row of filter controls (UI-011/§13.7): a search input plus
  a slot for additional typed filter controls (e.g. a status select),
  collapsing into a stacked compact form below 640px rather than
  squeezing controls into one unreadable row.
-->
<div class="filter-bar">
  <input
    type="search"
    class="search"
    placeholder={searchPlaceholder}
    value={searchValue}
    oninput={onInput}
    aria-label={searchPlaceholder}
  />
  {#if filters}
    <div class="filters">
      {@render filters()}
    </div>
  {/if}
</div>

<style>
  .filter-bar {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .search {
    flex: 1;
    min-width: 160px;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: 0.4rem 0.6rem;
    background: var(--color-surface);
    color: var(--color-text);
    font-size: 0.85rem;
  }
  .filters {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }

  @media (max-width: 639px) {
    .filter-bar {
      flex-direction: column;
      align-items: stretch;
    }
    .search {
      min-width: 0;
      width: 100%;
    }
    .filters {
      width: 100%;
      flex-direction: column;
      align-items: stretch;
    }
  }
</style>
