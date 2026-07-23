<script lang="ts">
  import ChartCard from "../../src/lib/components/cards/ChartCard.svelte";

  interface Props {
    title?: string;
    description?: string;
    withContent?: boolean;
  }
  let { title = "Review activity trend", description, withContent = false }: Props = $props();
</script>

{#if withContent}
  <ChartCard {title} {description}>
    {#snippet children()}
      <p data-testid="chart-card-slot-content">Chart goes here</p>
    {/snippet}
  </ChartCard>
{:else}
  <ChartCard {title} {description} />
{/if}
