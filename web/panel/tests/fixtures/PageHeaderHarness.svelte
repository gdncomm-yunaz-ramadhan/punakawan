<script lang="ts">
  import PageHeader from "../../src/lib/components/PageHeader.svelte";

  interface Props {
    title: string;
    description?: string;
    withActions?: boolean;
  }
  let { title, description, withActions = false }: Props = $props();
</script>

{#if withActions}
  <PageHeader {title} {description}>
    {#snippet actions()}
      <button type="button">Do thing</button>
    {/snippet}
  </PageHeader>
{:else}
  <PageHeader {title} {description} />
{/if}
