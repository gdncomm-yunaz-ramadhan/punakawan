<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import type { Core, CoseLayoutOptions, ElementDefinition, LayoutOptions } from "cytoscape";
  import {
    prefersReducedMotion,
    readThemePalette,
    watchReducedMotionChange,
    watchThemeChange,
  } from "../charts/themeColors";
  import { loadCytoscape } from "./graphLoader";
  import { buildGraphStylesheet } from "./graphStyle";
  import { DEFAULT_NODE_CAP, type GraphEdge, type GraphLayoutName, type GraphNode } from "./types";

  export interface GraphCanvasApi {
    zoomIn: () => void;
    zoomOut: () => void;
    fit: () => void;
    runLayout: (name: GraphLayoutName) => void;
  }

  interface Props {
    nodes: GraphNode[];
    edges: GraphEdge[];
    /** Concise label describing what this graph shows, for the canvas's aria-label. */
    ariaLabel: string;
    /** Node beyond which the graph refuses to render and shows the "too large" state instead. */
    nodeCap?: number;
    layoutName?: GraphLayoutName;
    /** Id of a node to visually mark as the focus (e.g. the subject of a focused-subgraph view). */
    focusNodeId?: string;
    height?: number;
    onNodeSelect?: (nodeId: string) => void;
    onNodeExpand?: (nodeId: string) => void;
    /** Exposes the Cytoscape core instance to a parent (e.g. GraphControls) once ready. */
    onReady?: (api: GraphCanvasApi | null) => void;
  }
  let {
    nodes,
    edges,
    ariaLabel,
    nodeCap = DEFAULT_NODE_CAP,
    layoutName = "cose",
    focusNodeId,
    height = 360,
    onNodeSelect,
    onNodeExpand,
    onReady,
  }: Props = $props();

  let containerEl: HTMLDivElement | undefined = $state();
  let cy: Core | null = null;
  let loading = $state(true);
  let loadError: string | null = $state(null);

  const tooLarge = $derived(nodes.length > nodeCap);

  let unwatchTheme: (() => void) | null = null;
  let unwatchMotion: (() => void) | null = null;

  // Node ids currently represented in `cy` - null whenever there's no live
  // instance to diff against (before first render, or after a tooLarge/
  // error state left `cy` pointing at a detached container). Tracked
  // separately from `nodes` so the effect can tell "same underlying graph,
  // different focus/expand bounds" apart from "genuinely new dataset".
  let renderedNodeIds: Set<string> | null = null;
  let appliedLayoutName: GraphLayoutName | null = null;
  let appliedFocusNodeId: string | undefined;

  function toElements(): ElementDefinition[] {
    const nodeIds = new Set(nodes.map((n) => n.id));
    const els: ElementDefinition[] = nodes.map((n) => ({
      data: { id: n.id, label: n.label, type: n.type, ...n.data },
      classes: focusNodeId && n.id === focusNodeId ? "focus-node" : undefined,
    }));
    for (const e of edges) {
      // Skip dangling edges referencing nodes outside the current bounded set -
      // this keeps the canvas and the relation-list fallback in sync with
      // exactly the same (possibly focused/capped) subset.
      if (!nodeIds.has(e.source) || !nodeIds.has(e.target)) continue;
      els.push({ data: { id: e.id, source: e.source, target: e.target, label: e.label, type: e.type } });
    }
    return els;
  }

  function layoutOptions(name: GraphLayoutName, reducedMotion: boolean): LayoutOptions {
    const base = { name, animate: !reducedMotion, animationDuration: reducedMotion ? 0 : 300 };
    if (name === "breadthfirst") {
      return { ...base, directed: true, spacingFactor: 1.45, circle: false } as LayoutOptions;
    }
    return { ...base, nodeRepulsion: 9000, idealEdgeLength: 135, nodeOverlap: 24 } as LayoutOptions;
  }

  async function render() {
    if (tooLarge) {
      loading = false;
      return;
    }
    try {
      const cytoscape = await loadCytoscape();
      loading = false;
      if (!containerEl) return;
      const palette = readThemePalette();
      const reducedMotion = prefersReducedMotion();
      if (cy) {
        cy.destroy();
        cy = null;
      }
      cy = cytoscape({
        container: containerEl,
        elements: toElements(),
        style: buildGraphStylesheet(palette),
        layout: layoutOptions(layoutName, reducedMotion),
        minZoom: 0.1,
        maxZoom: 4,
        // Keeps panning/dragging smooth as node count grows toward the
        // cap: edges disappear (cheaply) during viewport transforms, and
        // the whole graph rasterizes to a texture while it's moving.
        hideEdgesOnViewport: true,
        textureOnViewport: true,
      });
      cy.on("tap", "node", (evt) => {
        onNodeSelect?.(evt.target.id());
      });
      cy.on("dbltap", "node", (evt) => {
        onNodeExpand?.(evt.target.id());
      });
      renderedNodeIds = new Set(nodes.map((n) => n.id));
      appliedLayoutName = layoutName;
      appliedFocusNodeId = focusNodeId;
      publishApi();
    } catch (e) {
      loading = false;
      loadError = e instanceof Error ? e.message : String(e);
    }
  }

  // Updates the live instance's elements instead of tearing it down - the
  // common case (focus/expand changing which bounded subset of the same
  // graph is visible) shouldn't discard positions the user just dragged a
  // node to, nor pay for a full teardown + fresh layout. Only elements
  // that actually left/entered the visible set are touched; everything
  // else keeps its current position untouched.
  function diffAndUpdate() {
    if (!cy) return;
    const newElements = toElements();
    const newElementIds = new Set(newElements.map((el) => (el.data as { id: string }).id));

    const stale = cy.elements().filter((ele) => !newElementIds.has(ele.id()));
    if (stale.length > 0) cy.remove(stale);

    const additions = newElements.filter((el) => cy!.getElementById((el.data as { id: string }).id).empty());
    const added = additions.length > 0 ? cy.add(additions) : cy.collection();

    // add()/remove() alone won't restyle a node that was already present
    // and simply stopped/started being the focus node - flip its class
    // directly instead of touching every node on every diff.
    if (appliedFocusNodeId !== focusNodeId) {
      if (appliedFocusNodeId) cy.getElementById(appliedFocusNodeId).removeClass("focus-node");
      if (focusNodeId) cy.getElementById(focusNodeId).addClass("focus-node");
      appliedFocusNodeId = focusNodeId;
    }

    if (layoutName !== appliedLayoutName) {
      // A deliberate layout-algorithm switch (via GraphControls) - a full
      // re-arrangement is the point, unlike the incidental focus/expand
      // churn this diffing exists to avoid disturbing.
      cy.layout(layoutOptions(layoutName, prefersReducedMotion())).run();
      appliedLayoutName = layoutName;
    } else {
      const addedNodes = added.nodes();
      if (addedNodes.length > 0) {
        // Lay out only the newly-added elements, starting from their
        // current (default/stacked) positions rather than re-scattering
        // the nodes that were already placed.
        const opts = layoutOptions(layoutName, prefersReducedMotion());
        if (opts.name === "cose") (opts as CoseLayoutOptions).randomize = false;
        added.layout(opts).run();
      }
    }

    renderedNodeIds = new Set(nodes.map((n) => n.id));
  }

  // "Same dataset" means the new node set shares at least one id with what's
  // currently rendered - i.e. this is the same underlying graph with a
  // different focus/expand boundary, not a wholesale replacement.
  function isSameDataset(newNodeIds: Set<string>): boolean {
    if (!renderedNodeIds || renderedNodeIds.size === 0) return false;
    for (const id of newNodeIds) {
      if (renderedNodeIds.has(id)) return true;
    }
    return false;
  }

  function recolor() {
    if (!cy) return;
    const palette = readThemePalette();
    cy.style(buildGraphStylesheet(palette)).update();
  }

  function publishApi() {
    if (!onReady) return;
    if (!cy) {
      onReady(null);
      return;
    }
    const instance = cy;
    onReady({
      zoomIn: () => instance.zoom(instance.zoom() * 1.2),
      zoomOut: () => instance.zoom(instance.zoom() / 1.2),
      fit: () => instance.fit(undefined, 24),
      runLayout: (name: GraphLayoutName) => {
        // Re-running a layout on an already-positioned graph (e.g. the
        // user re-triggering the current layout) shouldn't re-scatter
        // everything cose already settled - only a fresh cy.destroy()+
        // reconstruct (first mount, new dataset) should randomize.
        const opts = layoutOptions(name, prefersReducedMotion());
        if (opts.name === "cose") (opts as CoseLayoutOptions).randomize = false;
        instance.layout(opts).run();
        appliedLayoutName = name;
      },
    });
  }

  onMount(() => {
    render();
    unwatchTheme = watchThemeChange(recolor);
    unwatchMotion = watchReducedMotionChange(() => {
      if (cy) cy.layout(layoutOptions(layoutName, prefersReducedMotion())).run();
    });
  });

  onDestroy(() => {
    unwatchTheme?.();
    unwatchMotion?.();
    onReady?.(null);
    cy?.destroy();
    cy = null;
  });

  $effect(() => {
    nodes;
    edges;
    layoutName;
    focusNodeId;
    if (loading) return;
    if (tooLarge) {
      // Matches render()'s own early return: cy (if any) is about to be
      // orphaned behind the "too large" state, so nothing left to diff
      // against once we're back under the cap.
      renderedNodeIds = null;
      render();
      return;
    }
    const newNodeIds = new Set(nodes.map((n) => n.id));
    if (cy && isSameDataset(newNodeIds)) {
      diffAndUpdate();
    } else {
      render();
    }
  });
</script>

<div class="graph-canvas-wrap" style={`--graph-height: ${height}px`}>
  {#if tooLarge}
    <div class="too-large" role="status">
      <p>
        This graph has {nodes.length} nodes, which is over the {nodeCap}-node display limit.
      </p>
      <p class="hint">
        Narrow the view - focus on a single node and expand its neighbors, or filter to a smaller subset -
        before rendering the full diagram.
      </p>
    </div>
  {:else if loading}
    <div class="skeleton" role="status" aria-live="polite">
      <span class="sr-only">Loading graph…</span>
    </div>
  {:else if loadError}
    <p role="alert" class="graph-error">Graph failed to load: {loadError}</p>
  {:else}
    <div bind:this={containerEl} class="cy-container" role="img" aria-label={ariaLabel}></div>
  {/if}
</div>

<style>
  .graph-canvas-wrap {
    position: relative;
  }
  .cy-container {
    width: 100%;
    height: var(--graph-height);
    border: 1px solid var(--surface-card-border, var(--color-border));
    border-radius: var(--radius-card);
    background-color: var(--color-surface);
    background-image:
      linear-gradient(color-mix(in srgb, var(--color-border) 24%, transparent) 1px, transparent 1px),
      linear-gradient(90deg, color-mix(in srgb, var(--color-border) 24%, transparent) 1px, transparent 1px);
    background-size: 24px 24px;
    box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.5), var(--shadow-sm);
  }
  .skeleton {
    width: 100%;
    height: var(--graph-height);
    border-radius: var(--radius-card);
    background: linear-gradient(
      90deg,
      var(--color-surface-subtle) 25%,
      var(--color-surface-raised) 37%,
      var(--color-surface-subtle) 63%
    );
    background-size: 400% 100%;
  }
  @media (prefers-reduced-motion: no-preference) {
    .skeleton {
      animation: shimmer 1.4s ease infinite;
    }
  }
  @keyframes shimmer {
    0% {
      background-position: 100% 50%;
    }
    100% {
      background-position: 0 50%;
    }
  }
  .graph-error {
    color: var(--color-danger);
    font-size: 0.85rem;
  }
  .too-large {
    min-height: var(--graph-height);
    display: grid;
    align-content: center;
    gap: 0.4rem;
    border: 1px dashed var(--color-border-strong);
    border-radius: var(--radius-card);
    background: var(--color-surface-subtle);
    color: var(--color-text);
    padding: 1.5rem;
    text-align: center;
  }
  .too-large p {
    margin: 0;
  }
  .hint {
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
</style>
