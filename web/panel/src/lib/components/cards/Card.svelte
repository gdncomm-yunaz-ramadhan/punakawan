<script lang="ts">
  import type { Snippet } from "svelte";

  export type CardState = "default" | "loading" | "empty" | "warning" | "error";

  interface Props {
    state?: CardState;
    emptyMessage?: string;
    warningMessage?: string;
    errorMessage?: string;
    header?: Snippet;
    footer?: Snippet;
    children?: Snippet;
  }
  let {
    state = "default",
    emptyMessage = "Nothing to show yet.",
    warningMessage = "This needs attention.",
    errorMessage = "Something went wrong.",
    header,
    footer,
    children,
  }: Props = $props();
</script>

<!--
  Base surface for every card in the system (UI-010, §13.6). Owns
  background/border/radius/shadow/padding and the four non-default
  states so no feature page reimplements skeleton/empty/warning/error
  markup on its own.
-->
<div class="card">
  {#if header}
    <div class="card-header">{@render header()}</div>
  {/if}

  <div class="card-body">
    {#if state === "loading"}
      <div class="skeleton" role="status" aria-label="Loading" data-testid="card-skeleton">
        <span class="skeleton-line long"></span>
        <span class="skeleton-line"></span>
        <span class="skeleton-line short"></span>
      </div>
    {:else if state === "empty"}
      <p class="empty-message" data-testid="card-empty">{emptyMessage}</p>
    {:else if state === "warning"}
      <p class="banner banner-warning" role="status" data-testid="card-warning">{warningMessage}</p>
    {:else if state === "error"}
      <p class="banner banner-error" role="alert" data-testid="card-error">{errorMessage}</p>
    {:else if children}
      {@render children()}
    {/if}
  </div>

  {#if footer}
    <div class="card-footer">{@render footer()}</div>
  {/if}
</div>

<style>
  .card {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 1rem 1.1rem;
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
    box-sizing: border-box;
    height: 100%;
    min-width: 0;
  }
  .card-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.5rem;
  }
  .card-body {
    flex: 1;
    min-width: 0;
  }
  .card-footer {
    border-top: 1px solid var(--color-border);
    padding-top: 0.5rem;
  }

  .skeleton {
    display: grid;
    gap: 0.5rem;
  }
  .skeleton-line {
    display: block;
    height: 0.85rem;
    border-radius: 4px;
    background: var(--color-surface-subtle);
  }
  .skeleton-line.long {
    width: 80%;
  }
  .skeleton-line.short {
    width: 45%;
  }
  @media (prefers-reduced-motion: no-preference) {
    .skeleton-line {
      animation: pulse 1.1s ease-in-out infinite;
    }
  }
  @keyframes pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.5;
    }
  }

  .empty-message {
    color: var(--color-text-muted);
    font-size: 0.85rem;
    margin: 0;
  }

  .banner {
    margin: 0;
    font-size: 0.85rem;
    padding: 0.5rem 0.65rem;
    border-radius: 8px;
  }
  .banner-warning {
    background: var(--color-accent-soft);
    color: var(--color-warning);
  }
  .banner-error {
    background: var(--color-accent-soft);
    color: var(--color-danger);
  }
</style>
