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

<!--
  Bottom-anchored overlay with rounded top corners and a drag-handle
  visual affordance (UI-007) - visually distinct from Drawer (side-anchored)
  and Dialog (centered), per the plan. This is the mobile-appropriate
  primitive §13.4 refers to as "detail drawers become bottom sheets on
  mobile" for future call sites; it does not auto-swap with Drawer itself.
-->
<svelte:window onkeydown={open ? onKeydown : undefined} />

{#if open}
  <div class="backdrop" role="presentation" onclick={onclose}></div>
  <div class="sheet" role="dialog" aria-modal="true" aria-label={title ?? "Sheet"}>
    <div class="handle" aria-hidden="true"></div>
    <div class="sheet-head">
      {#if title}<h2>{title}</h2>{/if}
      <button type="button" class="close" onclick={onclose} aria-label="Close">✕</button>
    </div>
    <div class="sheet-body">
      {@render children()}
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
  .sheet {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 0;
    max-height: min(80vh, 640px);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-bottom: none;
    border-top-left-radius: var(--radius-card);
    border-top-right-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 0.5rem 1.25rem 1.25rem;
    overflow-y: auto;
    z-index: 31;
    box-sizing: border-box;
    padding-bottom: calc(1.25rem + env(safe-area-inset-bottom, 0px));
  }
  .handle {
    width: 40px;
    height: 4px;
    border-radius: 2px;
    background: var(--color-border-strong);
    margin: 0.5rem auto 0.75rem;
  }
  .sheet-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.75rem;
  }
  .sheet-head h2 {
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
  .sheet-body {
    color: var(--color-text);
  }

  @media (prefers-reduced-motion: no-preference) {
    .sheet {
      animation: slide-up 160ms ease-out;
    }
  }
  @keyframes slide-up {
    from {
      transform: translateY(16px);
      opacity: 0;
    }
    to {
      transform: translateY(0);
      opacity: 1;
    }
  }
</style>
