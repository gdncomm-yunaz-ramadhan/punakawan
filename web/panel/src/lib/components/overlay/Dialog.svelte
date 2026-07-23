<script lang="ts">
  import type { Snippet } from "svelte";

  interface Props {
    open: boolean;
    title?: string;
    children: Snippet;
    onclose: () => void;
  }
  let { open, title, children, onclose }: Props = $props();

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") onclose();
  }
</script>

<!-- Centered overlay primitive (UI-007) - see Drawer.svelte for the shared backdrop/escape pattern this generalizes. -->
<svelte:window onkeydown={open ? onKeydown : undefined} />

{#if open}
  <div class="backdrop" role="presentation" onclick={onclose}></div>
  <div class="dialog-wrap" role="presentation">
    <div class="dialog" role="dialog" aria-modal="true" aria-label={title ?? "Dialog"}>
      <div class="dialog-head">
        {#if title}<h2>{title}</h2>{/if}
        <button type="button" class="close" onclick={onclose} aria-label="Close">✕</button>
      </div>
      <div class="dialog-body">
        {@render children()}
      </div>
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.35);
    z-index: 30;
  }
  .dialog-wrap {
    position: fixed;
    inset: 0;
    z-index: 31;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
    box-sizing: border-box;
  }
  .dialog {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    width: min(480px, 100%);
    max-height: calc(100vh - 2rem);
    overflow-y: auto;
    padding: 1.25rem;
    box-sizing: border-box;
  }
  .dialog-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.75rem;
  }
  .dialog-head h2 {
    font-size: 1.05rem;
    margin: 0;
    color: var(--color-text);
  }
  .close {
    background: none;
    border: none;
    font-size: 1rem;
    cursor: pointer;
    color: var(--color-text);
    min-height: 44px;
    min-width: 44px;
  }
  .dialog-body {
    color: var(--color-text);
  }

  @media (prefers-reduced-motion: no-preference) {
    .dialog {
      animation: pop-in 140ms ease-out;
    }
  }
  @keyframes pop-in {
    from {
      transform: scale(0.97);
      opacity: 0;
    }
    to {
      transform: scale(1);
      opacity: 1;
    }
  }
</style>
