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

  // Manual node resize (§ readability follow-up). Bounds are generous
  // enough to shrink a node below its label-driven default (for a user
  // who wants a denser view) or grow it well past a typical wrapped
  // title, without letting a stray drag collapse a node to nothing or
  // blow it up past what the layout/canvas can sanely display.
  const MIN_NODE_WIDTH = 60;
  const MAX_NODE_WIDTH = 480;
  const MIN_NODE_HEIGHT = 40;
  const MAX_NODE_HEIGHT = 320;
  const RESIZE_KEY_STEP = 12;

  // Id of the node currently showing a resize handle - deliberately
  // separate state from the caller's onNodeSelect callback (that's the
  // parent view's own "which row is highlighted" concern); this is purely
  // an in-canvas UI affordance, same footing as focus/position.
  let resizeNodeId: string | null = $state(null);
  // Screen-space (container-relative) coordinates of the handle, derived
  // from the resized node's rendered position/size. Null whenever nothing
  // is selected, so the template can skip rendering the handle entirely.
  let handlePos: { left: number; top: number } | null = $state(null);
  // Drag-in-progress bookkeeping - not reactive state, since nothing in
  // the template reads it directly (only handlePos/node data do).
  let dragStart: { startX: number; startY: number; startW: number; startH: number } | null = null;

  function clamp(value: number, min: number, max: number): number {
    return Math.min(max, Math.max(min, value));
  }

  // Recomputes the handle's on-screen position from the live node's
  // renderedPosition/renderedWidth/renderedHeight (i.e. already
  // pan/zoom-adjusted) - called after anything that could move the node
  // or the viewport, so the handle never lags behind what's on screen.
  function updateHandlePosition() {
    if (!cy || !resizeNodeId || !containerEl) {
      if (handlePos !== null) handlePos = null;
      return;
    }
    const node = cy.getElementById(resizeNodeId);
    if (node.empty()) {
      resizeNodeId = null;
      handlePos = null;
      return;
    }
    const pos = node.renderedPosition();
    const w = node.renderedWidth();
    const h = node.renderedHeight();
    // containerEl.clientLeft/Top is the container's own border width -
    // renderedPosition is relative to the canvas's drawing origin (inside
    // the border), while the handle is positioned relative to the same
    // offsetParent (.graph-canvas-wrap) the container itself sits in.
    const left = containerEl.offsetLeft + containerEl.clientLeft + pos.x + w / 2;
    const top = containerEl.offsetTop + containerEl.clientTop + pos.y + h / 2;
    // Skip the reassignment when nothing actually moved - this runs on
    // every "render" event (i.e. potentially every repaint), and an
    // unnecessary $state write per frame is exactly the kind of thing the
    // diffing work in this file exists to avoid.
    if (handlePos && handlePos.left === left && handlePos.top === top) return;
    handlePos = { left, top };
  }

  function selectForResize(nodeId: string | null) {
    resizeNodeId = nodeId;
    updateHandlePosition();
  }

  function beginResize(evt: PointerEvent) {
    if (!cy || !resizeNodeId) return;
    const node = cy.getElementById(resizeNodeId);
    if (node.empty()) return;
    evt.preventDefault();
    evt.stopPropagation();
    dragStart = { startX: evt.clientX, startY: evt.clientY, startW: node.width(), startH: node.height() };
    // Listen on window, not just the handle - a fast drag can easily
    // move the pointer past the handle's own (small) hit area between
    // frames, and a handle-only listener would then stop tracking.
    window.addEventListener("pointermove", onResizeMove);
    window.addEventListener("pointerup", endResize);
    window.addEventListener("pointercancel", endResize);
  }

  function onResizeMove(evt: PointerEvent) {
    if (!dragStart || !cy || !resizeNodeId) return;
    const node = cy.getElementById(resizeNodeId);
    if (node.empty()) {
      endResize();
      return;
    }
    // Pointer movement is in screen pixels; node dimensions are in
    // Cytoscape's model space, so a delta has to be un-zoomed before it's
    // added to the starting size, or resizing would feel wrong (too
    // fast/slow) at any zoom level other than 1.
    const zoom = cy.zoom() || 1;
    const width = clamp(dragStart.startW + (evt.clientX - dragStart.startX) / zoom, MIN_NODE_WIDTH, MAX_NODE_WIDTH);
    const height = clamp(dragStart.startH + (evt.clientY - dragStart.startY) / zoom, MIN_NODE_HEIGHT, MAX_NODE_HEIGHT);
    // A plain per-node data write. graphStyle.ts's node[width]/node[height]
    // selectors are data() mappers reading these same fields, and
    // Cytoscape's data() setter marks the element's own style dirty as
    // part of the write (see cytoscape's `updateStyle: true` data
    // definition) - so this alone repaints just this node. No
    // cy.style().update() call and no layout re-run, so every other
    // node's position is left untouched.
    node.data({ width, height });
    updateHandlePosition();
  }

  function endResize() {
    dragStart = null;
    window.removeEventListener("pointermove", onResizeMove);
    window.removeEventListener("pointerup", endResize);
    window.removeEventListener("pointercancel", endResize);
  }

  // Keyboard equivalent of the drag handle - arrow keys step the size up
  // or down while the handle has focus, so resizing isn't pointer-only.
  function resizeByKeyboard(evt: KeyboardEvent) {
    if (!cy || !resizeNodeId) return;
    const node = cy.getElementById(resizeNodeId);
    if (node.empty()) return;
    let dw = 0;
    let dh = 0;
    if (evt.key === "ArrowRight") dw = RESIZE_KEY_STEP;
    else if (evt.key === "ArrowLeft") dw = -RESIZE_KEY_STEP;
    else if (evt.key === "ArrowDown") dh = RESIZE_KEY_STEP;
    else if (evt.key === "ArrowUp") dh = -RESIZE_KEY_STEP;
    else return;
    evt.preventDefault();
    const width = clamp(node.width() + dw, MIN_NODE_WIDTH, MAX_NODE_WIDTH);
    const height = clamp(node.height() + dh, MIN_NODE_HEIGHT, MAX_NODE_HEIGHT);
    node.data({ width, height });
    updateHandlePosition();
  }

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
      // Nodes now size themselves to their own label (graphStyle.ts), so a
      // lineage mixing short ("v1") and long (full title) labels has
      // uneven heights across the same tier - a bit more spacing than the
      // old fixed-box tuning keeps adjacent tiers from crowding when one
      // of them happens to be full of tall, multi-line nodes.
      return { ...base, directed: true, spacingFactor: 1.6, circle: false } as LayoutOptions;
    }
    // These three were tuned for the old fixed 152x58 box. Nodes now
    // range from a small tidy box (short label) up to roughly 200-240px
    // wide by 80-120px tall (a sentence-length title wrapped at
    // text-max-width: 200px) - noticeably bigger on average, so the
    // forces that keep unconnected nodes apart and connected ones at a
    // sane distance both need to scale up with them, or the bigger boxes
    // just overlap/crowd where the old numbers assumed a smaller node.
    return { ...base, nodeRepulsion: 16000, idealEdgeLength: 220, nodeOverlap: 36 } as LayoutOptions;
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
      // A fresh instance means a fresh graph (or the very first render) -
      // any in-progress/previous resize target belongs to a cy that no
      // longer exists.
      endResize();
      resizeNodeId = null;
      handlePos = null;
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
        const nodeId = evt.target.id();
        onNodeSelect?.(nodeId);
        selectForResize(nodeId);
      });
      cy.on("dbltap", "node", (evt) => {
        onNodeExpand?.(evt.target.id());
      });
      // Tapping empty canvas bubbles here too (evt.target is the core
      // itself in that case, not an element) - the standard Cytoscape
      // idiom for detecting a background tap, used here to drop the
      // resize handle when the user taps away from the selected node.
      cy.on("tap", (evt) => {
        if (evt.target === cy) selectForResize(null);
      });
      // If the node under resize gets pruned by a later diffAndUpdate
      // (e.g. it fell out of the focused/bounded subgraph), drop the
      // handle instead of leaving it pointed at a removed element.
      cy.on("remove", "node", (evt) => {
        if (evt.target.id() === resizeNodeId) selectForResize(null);
      });
      // Keeps the overlay handle glued to the node's on-screen corner
      // through panning, zooming, and the node itself being dragged.
      cy.on("pan zoom position render", updateHandlePosition);
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
    // Covers the focus-node class flip above, which nudges border-width
    // and so the resized node's rendered corner by a pixel or two.
    updateHandlePosition();
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
    endResize();
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
    {#if handlePos}
      <!--
        Cytoscape renders to a <canvas> - there's no per-node DOM element to
        attach a native resize handle to - so this is a plain HTML overlay
        positioned from the selected node's renderedPosition/renderedWidth/
        renderedHeight and kept in sync via the cy "pan zoom position
        render" binding in the script above. role="button" plus arrow-key
        handling isn't a perfect ARIA fit for a 2D resize grip (no role
        models that precisely), but it's the closest native semantics and
        gives keyboard users a working equivalent to the drag.
      -->
      <div
        class="resize-handle"
        style={`left:${handlePos.left}px; top:${handlePos.top}px`}
        role="button"
        tabindex="0"
        aria-label="Resize node"
        onpointerdown={beginResize}
        onkeydown={resizeByKeyboard}
      ></div>
    {/if}
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
  .resize-handle {
    position: absolute;
    width: 14px;
    height: 14px;
    /* left/top mark the node's corner point itself - center the handle
       on that point rather than anchoring its own top-left corner there. */
    transform: translate(-50%, -50%);
    border-radius: 4px;
    background: var(--color-accent);
    border: 2px solid var(--color-surface);
    box-shadow: var(--shadow-sm);
    cursor: nwse-resize;
    /* Without this, touch browsers can hijack the gesture as a page pan
       partway through a drag. */
    touch-action: none;
  }
  .resize-handle:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
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
