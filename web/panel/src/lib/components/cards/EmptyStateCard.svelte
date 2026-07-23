<script lang="ts">
  import type { Snippet } from "svelte";

  interface Props {
    title?: string;
    message?: string;
    action?: Snippet;
  }
  let { title = "Nothing here yet", message = "There is no data to show right now.", action }: Props = $props();
</script>

<!--
  Standalone empty state (UI-010), usable on its own for a full-page
  empty state rather than only inside a Card's `state` prop.
-->
<div class="empty-state">
  <p class="title">{title}</p>
  <p class="message">{message}</p>
  {#if action}
    <div class="action">{@render action()}</div>
  {/if}
</div>

<style>
  .empty-state {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 2rem 1.5rem;
    text-align: center;
    display: grid;
    gap: 0.4rem;
    justify-items: center;
  }
  .title {
    margin: 0;
    font-weight: 600;
    color: var(--color-text);
  }
  .message {
    margin: 0;
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
  .action {
    margin-top: 0.5rem;
  }
</style>
