<script lang="ts">
  import type { Snippet } from "svelte";
  import { tick } from "svelte";
  import Icon from "../Icon.svelte";

  interface Props {
    open: boolean;
    title?: string;
    children: Snippet;
    onclose: () => void;
  }
  let { open, title, children, onclose }: Props = $props();

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      onclose();
      return;
    }
    if (e.key === "Tab") trapTab(e);
  }

  let dialogEl: HTMLDivElement | undefined = $state();
  // Element focused immediately before this dialog opened - Escape/close/
  // backdrop-click all restore focus here so a keyboard or screen-reader
  // user lands back where they were, rather than on <body> (WCAG 2.4.3).
  let previouslyFocused: HTMLElement | null = null;

  function focusableElements(): HTMLElement[] {
    if (!dialogEl) return [];
    // Deliberately not filtering on offsetParent/getClientRects for
    // "visibility" - jsdom (this project's test environment) never
    // computes real layout, so offsetParent is always null there even
    // for genuinely visible elements. A real browser wouldn't put a
    // hidden interactive element inside an open dialog's content in the
    // first place.
    return Array.from(
      dialogEl.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ),
    );
  }

  function trapTab(e: KeyboardEvent) {
    const focusable = focusableElements();
    if (focusable.length === 0) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    const active = document.activeElement;
    if (e.shiftKey) {
      if (active === first || !dialogEl?.contains(active)) {
        e.preventDefault();
        last.focus();
      }
    } else {
      if (active === last || !dialogEl?.contains(active)) {
        e.preventDefault();
        first.focus();
      }
    }
  }

  // Move focus into the dialog on open, and restore it to whatever
  // triggered the dialog once it closes (focus trapping per §13.13
  // "mobile bottom sheets trap focus and restore it on close" - Dialog
  // gets the same treatment for consistency).
  $effect(() => {
    if (open) {
      previouslyFocused = document.activeElement as HTMLElement | null;
      tick().then(() => {
        const focusable = focusableElements();
        (focusable[0] ?? dialogEl)?.focus();
      });
    } else if (previouslyFocused) {
      previouslyFocused.focus();
      previouslyFocused = null;
    }
  });
</script>

<!-- Centered overlay primitive (UI-007) - see Drawer.svelte for the shared backdrop/escape pattern this generalizes. -->
<svelte:window onkeydown={open ? onKeydown : undefined} />

{#if open}
  <div class="backdrop" role="presentation" onclick={onclose}></div>
  <div class="dialog-wrap" role="presentation">
    <div class="dialog" role="dialog" aria-modal="true" aria-label={title ?? "Dialog"} tabindex="-1" bind:this={dialogEl}>
      <div class="dialog-head">
        {#if title}<h2>{title}</h2>{/if}
        <button type="button" class="close" onclick={onclose} aria-label="Close"><Icon name="x" size={18} /></button>
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
    background: rgb(10 18 30 / 0.54);
    backdrop-filter: blur(5px);
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
    overflow: hidden;
    background: var(--surface-card-bg, var(--color-surface));
    border: 1px solid var(--surface-card-border, var(--color-border));
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-lg);
    width: min(520px, 100%);
    max-height: calc(100vh - 2rem);
    overflow-y: auto;
    padding: 0;
    box-sizing: border-box;
  }
  .dialog-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    min-height: 58px;
    padding: 0.85rem 1rem;
    border-bottom: 1px solid var(--color-border);
    background: color-mix(in srgb, var(--color-surface-subtle) 76%, transparent);
  }
  .dialog-head h2 {
    font-size: 1.05rem;
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
  .close:hover {
    color: var(--color-accent);
    border-color: var(--color-accent);
  }
  .dialog-body {
    color: var(--color-text);
    padding: 1rem 1.15rem 1.15rem;
  }

  @media (prefers-reduced-motion: no-preference) {
    .dialog {
      animation: pop-in 140ms ease-out;
    }
  }
  @keyframes pop-in {
    from {
      transform: translateY(8px) scale(0.985);
      opacity: 0;
    }
    to {
      transform: scale(1);
      opacity: 1;
    }
  }
</style>
