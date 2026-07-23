<script lang="ts">
  import BentoCard, { type BentoSize } from "../../src/lib/components/cards/BentoCard.svelte";

  interface Props {
    size?: BentoSize;
    columns?: number;
  }
  let { size, columns }: Props = $props();
</script>

<BentoCard {size} {columns}>
  {#snippet children()}
    <p data-testid="bento-card-content">Bento card content</p>
  {/snippet}
</BentoCard>
