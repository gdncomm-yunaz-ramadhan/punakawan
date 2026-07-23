<script lang="ts">
  import type { GraphCanvasApi } from "./GraphCanvas.svelte";
  import type { GraphLayoutName } from "./types";

  interface Props {
    api: GraphCanvasApi | null;
    layoutName: GraphLayoutName;
    onLayoutChange: (name: GraphLayoutName) => void;
    /** Id of the currently selected node, if any - enables the "Expand" action. */
    selectedNodeId?: string | null;
    onExpand?: (nodeId: string) => void;
    disabled?: boolean;
  }
  let { api, layoutName, onLayoutChange, selectedNodeId = null, onExpand, disabled = false }: Props = $props();

  const LAYOUTS: { id: GraphLayoutName; label: string }[] = [
    { id: "cose", label: "Force-directed" },
    { id: "breadthfirst", label: "Hierarchical" },
  ];
</script>

<!--
  Zoom/fit/layout controls plus a focused-subgraph "Expand" action (§
  focused-subgraph-loading). Double-clicking a node in GraphCanvas triggers
  the same expand callback - this button is the discoverable, keyboard-
  reachable equivalent for anyone who can't or won't double-click.
-->
<div class="graph-controls" role="toolbar" aria-label="Graph controls">
  <div class="group" role="group" aria-label="Zoom">
    <button type="button" disabled={disabled || !api} onclick={() => api?.zoomIn()}>Zoom in</button>
    <button type="button" disabled={disabled || !api} onclick={() => api?.zoomOut()}>Zoom out</button>
    <button type="button" disabled={disabled || !api} onclick={() => api?.fit()}>Fit to view</button>
  </div>

  <label class="layout-select">
    <span>Layout</span>
    <select
      disabled={disabled || !api}
      value={layoutName}
      onchange={(e) => onLayoutChange((e.currentTarget as HTMLSelectElement).value as GraphLayoutName)}
    >
      {#each LAYOUTS as opt (opt.id)}
        <option value={opt.id}>{opt.label}</option>
      {/each}
    </select>
  </label>

  <button
    type="button"
    disabled={disabled || !selectedNodeId || !onExpand}
    onclick={() => selectedNodeId && onExpand?.(selectedNodeId)}
  >
    Expand selected node
  </button>
</div>

<style>
  .graph-controls {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem;
    margin-bottom: 0.5rem;
  }
  .group {
    display: flex;
    gap: 0.25rem;
  }
  button,
  select {
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    color: var(--color-text);
    border-radius: 6px;
    padding: 0.3rem 0.6rem;
    font-size: 0.8rem;
    cursor: pointer;
    min-height: 32px;
  }
  button:disabled,
  select:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .layout-select {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    font-size: 0.8rem;
    color: var(--color-text-muted);
  }
</style>
