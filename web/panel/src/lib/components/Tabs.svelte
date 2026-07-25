<script lang="ts">
  import Icon, { type IconName } from "./Icon.svelte";
  // Small reusable ARIA tabs primitive (punokawan-apy.7.3, mobile
  // proposal-review layout). No existing Tabs/TabList component covered
  // this: TasksPage.svelte's `role="tablist"` toolbar is just a button
  // row (no aria-selected/aria-controls/tabpanel/keyboard nav), so there
  // was nothing full-pattern to reuse from elsewhere in this codebase.
  // Follows the WAI-ARIA Tabs Pattern with the automatic-activation
  // model: arrow keys move focus and select in the same step (Home/End
  // jump to the first/last tab); a plain Tab key leaves the tablist for
  // whatever the caller renders next, same as any native control.
  interface Tab {
    id: string;
    label: string;
    icon?: IconName;
  }
  interface Props {
    tabs: Tab[];
    activeId: string;
    onchange: (id: string) => void;
    ariaLabel: string;
  }
  let { tabs, activeId, onchange, ariaLabel }: Props = $props();

  let tabRefs: (HTMLButtonElement | undefined)[] = $state([]);

  function activate(index: number) {
    const tab = tabs[index];
    if (tab) onchange(tab.id);
  }

  function onKeydown(e: KeyboardEvent, index: number) {
    let nextIndex: number | null = null;
    if (e.key === "ArrowRight") nextIndex = (index + 1) % tabs.length;
    else if (e.key === "ArrowLeft") nextIndex = (index - 1 + tabs.length) % tabs.length;
    else if (e.key === "Home") nextIndex = 0;
    else if (e.key === "End") nextIndex = tabs.length - 1;
    if (nextIndex === null) return;
    e.preventDefault();
    activate(nextIndex);
    tabRefs[nextIndex]?.focus();
  }
</script>

<!--
  Caller owns the tabpanel(s) - this component only renders the tablist
  row. Each button's id/aria-controls follow the `tab-${id}`/
  `tabpanel-${id}` convention the caller is expected to mirror on its own
  panel element(s).
-->
<div class="tabs" role="tablist" aria-label={ariaLabel}>
  {#each tabs as tab, i (tab.id)}
    <button
      type="button"
      role="tab"
      id={`tab-${tab.id}`}
      aria-selected={activeId === tab.id}
      aria-controls={`tabpanel-${tab.id}`}
      tabindex={activeId === tab.id ? 0 : -1}
      class="tab"
      class:active={activeId === tab.id}
      bind:this={tabRefs[i]}
      onclick={() => activate(i)}
      onkeydown={(e) => onKeydown(e, i)}
      data-testid={`tab-${tab.id}`}
    >
      {#if tab.icon}<Icon name={tab.icon} size={16} />{/if}
      {tab.label}
    </button>
  {/each}
</div>

<style>
  .tabs {
    display: flex;
    gap: 0.4rem;
    overflow-x: auto;
    /* The row scrolls horizontally, so tabs never need to cram to fit -
       give each room to breathe and don't stretch them to fill. */
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    background: color-mix(in srgb, var(--color-surface) 88%, transparent);
    padding: 0.4rem 0.5rem;
    margin-bottom: 1.15rem;
    box-shadow: var(--shadow-sm);
    scrollbar-width: thin;
  }
  .tab {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.4rem;
    flex: 0 0 auto;
    border: none;
    background: none;
    color: var(--color-text-muted);
    font-size: 0.85rem;
    font-weight: 600;
    padding: 0.55rem 0.95rem;
    min-height: 44px;
    cursor: pointer;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    white-space: nowrap;
  }
  .tab.active {
    color: var(--color-accent);
    border-color: color-mix(in srgb, var(--color-accent) 22%, var(--color-border));
    background: var(--color-surface);
    box-shadow: var(--shadow-sm);
  }
  .tab:hover:not(.active) {
    color: var(--color-text);
    background: var(--color-surface-subtle);
  }
  .tab:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: -2px;
  }

  @media (prefers-reduced-motion: no-preference) {
    .tab {
      transition:
        color 120ms ease,
        border-color 120ms ease;
    }
  }
</style>
