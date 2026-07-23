<script lang="ts">
  import FilterBar from "../../src/lib/components/data/FilterBar.svelte";

  interface Props {
    withFilters?: boolean;
    onSearchChange?: (value: string) => void;
  }
  let { withFilters = false, onSearchChange }: Props = $props();

  let searchValue = $state("");
</script>

<FilterBar bind:searchValue searchPlaceholder="Search reviews" {onSearchChange}>
  {#snippet filters()}
    {#if withFilters}
      <select data-testid="status-filter">
        <option value="">Any status</option>
      </select>
    {/if}
  {/snippet}
</FilterBar>

<p data-testid="filter-bar-value">{searchValue}</p>
