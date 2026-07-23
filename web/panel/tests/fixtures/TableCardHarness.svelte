<script lang="ts">
  import TableCard from "../../src/lib/components/cards/TableCard.svelte";

  interface Props {
    title?: string;
    withViewAll?: boolean;
  }
  let { title = "Current revisions", withViewAll = false }: Props = $props();
</script>

<TableCard {title}>
  {#snippet viewAllAction()}
    {#if withViewAll}
      <button type="button">View all</button>
    {/if}
  {/snippet}
  {#snippet children()}
    <p data-testid="table-card-slot-content">Table goes here</p>
  {/snippet}
</TableCard>
