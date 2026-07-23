<script lang="ts">
  import GraphCard from "../../src/lib/components/cards/GraphCard.svelte";

  interface Props {
    title?: string;
    description?: string;
    withContent?: boolean;
  }
  let { title = "Artifact relations", description, withContent = false }: Props = $props();
</script>

{#if withContent}
  <GraphCard {title} {description}>
    {#snippet children()}
      <p data-testid="graph-card-slot-content">Graph goes here</p>
    {/snippet}
  </GraphCard>
{:else}
  <GraphCard {title} {description} />
{/if}
