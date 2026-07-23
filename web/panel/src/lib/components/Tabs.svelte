<script lang="ts">
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
      {tab.label}
    </button>
  {/each}
</div>

<style>
  .tabs {
    display: flex;
    gap: 0.25rem;
    overflow-x: auto;
    border-bottom: 1px solid var(--color-border);
    margin-bottom: 1rem;
  }
  .tab {
    flex: 1 0 auto;
    border: none;
    background: none;
    color: var(--color-text-muted);
    font-size: 0.85rem;
    font-weight: 600;
    padding: 0.5rem 0.75rem;
    min-height: 44px;
    cursor: pointer;
    border-bottom: 2px solid transparent;
    white-space: nowrap;
  }
  .tab.active {
    color: var(--color-accent);
    border-bottom-color: var(--color-accent);
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
