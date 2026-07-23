<script lang="ts">
  import BentoGrid from "../../src/lib/components/cards/BentoGrid.svelte";
</script>

<BentoGrid>
  {#snippet children()}
    <p data-testid="bento-grid-content">Grid content</p>
  {/snippet}
</BentoGrid>
