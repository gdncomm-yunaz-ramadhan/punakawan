<script lang="ts">
  import type { Snippet } from "svelte";

  export interface ToolbarAction {
    id: string;
    label: string;
    onSelect: () => void;
    disabled?: boolean;
  }

  interface Props {
    actions: ToolbarAction[];
    // Actions beyond this count collapse into the overflow menu. Callers
    // with few actions (<= visibleCount) never see a "More" trigger.
    visibleCount?: number;
    leading?: Snippet;
  }
  let { actions, visibleCount = 3, leading }: Props = $props();

  let menuOpen = $state(false);

  const visible = $derived(actions.slice(0, visibleCount));
  const overflow = $derived(actions.slice(visibleCount));

  function toggleMenu() {
    menuOpen = !menuOpen;
  }

  function select(action: ToolbarAction) {
    menuOpen = false;
    action.onSelect();
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") menuOpen = false;
  }
</script>

<!--
  A real collapse, not a static row (UI-006): actions beyond visibleCount
  move into a "More" menu rather than being hidden entirely, so nothing
  becomes unreachable on narrow layouts (§13.4: "Toolbars wrap or collapse
  into an overflow menu").
-->
<div class="toolbar" role="toolbar" aria-label="Actions" tabindex="-1" onkeydown={onKeydown}>
  {#if leading}
    <div class="leading">{@render leading()}</div>
  {/if}
  <div class="visible-actions">
    {#each visible as action (action.id)}
      <button type="button" class="action" disabled={action.disabled} onclick={() => select(action)}>
        {action.label}
      </button>
    {/each}
  </div>

  {#if overflow.length > 0}
    <div class="overflow">
      <button
        type="button"
        class="more-trigger"
        aria-haspopup="menu"
        aria-expanded={menuOpen}
        data-testid="toolbar-overflow-trigger"
        onclick={toggleMenu}
      >
        More ⋯
      </button>
      {#if menuOpen}
        <div class="menu" role="menu">
          {#each overflow as action (action.id)}
            <button
              type="button"
              role="menuitem"
              class="menu-item"
              disabled={action.disabled}
              onclick={() => select(action)}
            >
              {action.label}
            </button>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .toolbar {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: nowrap;
    position: relative;
  }
  .leading {
    display: flex;
    align-items: center;
  }
  .visible-actions {
    display: flex;
    gap: 0.4rem;
    flex-wrap: wrap;
  }
  .action,
  .more-trigger,
  .menu-item {
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    color: var(--color-text);
    border-radius: 6px;
    padding: 0.4rem 0.7rem;
    font-size: 0.85rem;
    cursor: pointer;
    min-height: 36px;
  }
  .action:hover,
  .more-trigger:hover,
  .menu-item:hover {
    border-color: var(--color-border-strong);
  }
  .action:disabled,
  .menu-item:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .overflow {
    position: relative;
  }
  .menu {
    position: absolute;
    top: calc(100% + 4px);
    right: 0;
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    background: var(--color-surface-raised);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    box-shadow: var(--shadow-card);
    padding: 0.35rem;
    z-index: 5;
    min-width: 160px;
  }
  .menu-item {
    border: none;
    text-align: left;
    width: 100%;
  }
</style>
