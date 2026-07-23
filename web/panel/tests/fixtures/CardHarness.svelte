<script lang="ts">
  import Card, { type CardState } from "../../src/lib/components/cards/Card.svelte";

  interface Props {
    state?: CardState;
    emptyMessage?: string;
    warningMessage?: string;
    errorMessage?: string;
  }
  let { state = "default", emptyMessage, warningMessage, errorMessage }: Props = $props();
</script>

<Card {state} {emptyMessage} {warningMessage} {errorMessage}>
  {#snippet children()}
    <p data-testid="card-content">Card content</p>
  {/snippet}
</Card>
