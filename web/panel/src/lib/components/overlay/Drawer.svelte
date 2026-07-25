<script lang="ts">
  import type { Snippet } from "svelte";
  import Icon from "../Icon.svelte";

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
  Generalizes the backdrop + close-button pattern already used by
  routes/tasks/TaskDetailDrawer.svelte (UI-007) into a reusable,
  token-based primitive. TaskDetailDrawer itself is intentionally left
  unmigrated (out of scope, per the plan) to avoid blast radius.
-->
<svelte:window onkeydown={open ? onKeydown : undefined} />

{#if open}
  <div class="backdrop" role="presentation" onclick={onclose}></div>
  <aside class="drawer" aria-label={title ?? "Drawer"}>
    <div class="drawer-head">
      {#if title}<h2>{title}</h2>{/if}
      <button type="button" class="close" onclick={onclose} aria-label="Close"><Icon name="x" size={18} /></button>
    </div>
    <div class="drawer-body">
      {@render children()}
    </div>
  </aside>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgb(10 18 30 / 0.54);
    backdrop-filter: blur(5px);
    z-index: 30;
  }
  .drawer {
    position: fixed;
    top: 0;
    right: 0;
    height: 100vh;
    width: min(460px, 100vw);
    background: var(--surface-card-bg, var(--color-surface-raised));
    border-left: 1px solid var(--color-border);
    border-top-left-radius: var(--radius-lg);
    border-bottom-left-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    padding: 0;
    overflow-y: auto;
    z-index: 31;
    box-sizing: border-box;
  }
  .drawer-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    min-height: 60px;
    padding: 0.85rem 1rem;
    border-bottom: 1px solid var(--color-border);
  }
  .drawer-head h2 {
    font-size: 1rem;
    margin: 0;
    color: var(--color-text);
  }
  .close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    font-size: 1rem;
    cursor: pointer;
    color: var(--color-text);
    min-height: 34px;
    min-width: 34px;
  }
  .drawer-body {
    color: var(--color-text);
    padding: 1rem 1.15rem;
  }

  @media (prefers-reduced-motion: no-preference) {
    .drawer {
      animation: slide-in 160ms ease-out;
    }
  }
  @keyframes slide-in {
    from {
      transform: translateX(16px);
      opacity: 0;
    }
    to {
      transform: translateX(0);
      opacity: 1;
    }
  }
</style>
