<script lang="ts">
  import type { Snippet } from "svelte";

  interface Props {
    title?: string;
    message?: string;
    action?: Snippet;
  }
  let { title = "Something went wrong", message = "Failed to load this data.", action }: Props = $props();
</script>

<!--
  Standalone error state (UI-010), usable on its own (e.g. a full-page
  load failure) rather than only inside a Card's `state` prop.
-->
<div class="error-state" role="alert">
  <p class="title">{title}</p>
  <p class="message">{message}</p>
  {#if action}
    <div class="action">{@render action()}</div>
  {/if}
</div>

<style>
  .error-state {
    background: var(--color-surface);
    border: 1px solid var(--color-danger);
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
    color: var(--color-danger);
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
