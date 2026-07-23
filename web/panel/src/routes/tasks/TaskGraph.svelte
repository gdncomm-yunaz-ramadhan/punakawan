<script lang="ts">
  import { onMount, tick } from "svelte";
  import type { TaskGraph } from "../../lib/api/client";
  import { externalRefs, layoutGraph } from "../../lib/graph/layout";

  interface Props {
    graph: TaskGraph;
    onselect: (id: string) => void;
  }
  let { graph, onselect }: Props = $props();

  let container: HTMLDivElement | undefined = $state();
  let lines: { x1: number; y1: number; x2: number; y2: number; cyclic: boolean }[] = $state([]);

  const nodeIds = $derived(graph.nodes.map((n) => n.id));
  const layout = $derived(layoutGraph(nodeIds, graph.edges));
  const externals = $derived(externalRefs(nodeIds, graph.edges));
  const cycleNodeIds = $derived(new Set(graph.cycles.flat()));

  const columns = $derived.by(() => {
    const cols: { id: string; external?: boolean }[][] = Array.from({ length: layout.maxLevel + 1 }, () => []);
    for (const node of graph.nodes) {
      const level = layout.levels.get(node.id) ?? 0;
      cols[level].push({ id: node.id });
    }
    // externals have no dependencies of their own, so they render as
    // level-0 leaves alongside real nodes with no dependencies.
    for (const ext of externals) {
      cols[0].push({ id: ext, external: true });
    }
    return cols;
  });

  function nodeById(id: string) {
    return graph.nodes.find((n) => n.id === id);
  }

  async function recomputeLines() {
    await tick();
    if (!container) return;
    const containerRect = container.getBoundingClientRect();
    const next: typeof lines = [];
    const cyclicPairs = new Set<string>();
    for (const cycle of graph.cycles) {
      for (let i = 0; i < cycle.length - 1; i++) {
        cyclicPairs.add(cycle[i] + "->" + cycle[i + 1]);
      }
    }
    for (const edge of graph.edges) {
      const fromEl = container.querySelector(`[data-node-id="${cssEscape(edge.from)}"]`);
      const toEl = container.querySelector(`[data-node-id="${cssEscape(edge.to)}"]`);
      if (!fromEl || !toEl) continue;
      const fromRect = fromEl.getBoundingClientRect();
      const toRect = toEl.getBoundingClientRect();
      next.push({
        x1: fromRect.left - containerRect.left + fromRect.width / 2,
        y1: fromRect.top - containerRect.top + fromRect.height / 2,
        x2: toRect.left - containerRect.left + toRect.width / 2,
        y2: toRect.top - containerRect.top + toRect.height / 2,
        cyclic: cyclicPairs.has(edge.from + "->" + edge.to),
      });
    }
    lines = next;
  }

  function cssEscape(value: string): string {
    return value.replace(/["\\]/g, "\\$&");
  }

  onMount(() => {
    recomputeLines();
  });
  $effect(() => {
    columns;
    recomputeLines();
  });
</script>

{#if graph.cycles.length > 0}
  <p class="cycle-warning" role="alert">
    {graph.cycles.length} dependency cycle(s) detected: {graph.cycles.map((c) => c.join(" → ")).join("; ")}
  </p>
{/if}

<div class="graph-scroll">
  <div class="graph" bind:this={container}>
    <svg class="connectors" aria-hidden="true">
      {#each lines as line, i (i)}
        <line x1={line.x1} y1={line.y1} x2={line.x2} y2={line.y2} class:cyclic={line.cyclic} />
      {/each}
    </svg>
    {#each columns as col, i (i)}
      <div class="column">
        {#each col as entry (entry.id)}
          {#if entry.external}
            <div class="node node-external" data-node-id={entry.id} title="External reference">
              <span class="node-id">{entry.id}</span>
              <span class="node-state">External</span>
            </div>
          {:else}
            {@const node = nodeById(entry.id)}
            <button
              type="button"
              class="node node-{node?.board_status ?? ''}"
              class:cyclic={cycleNodeIds.has(entry.id)}
              data-node-id={entry.id}
              onclick={() => onselect(entry.id)}
            >
              <span class="node-id">{entry.id}</span>
              <span class="node-title">{node?.title}</span>
              <span class="node-state">{node?.board_status}</span>
            </button>
          {/if}
        {/each}
      </div>
    {/each}
  </div>
</div>

<section aria-labelledby="dependency-list-heading" class="fallback">
  <h3 id="dependency-list-heading">Dependency List</h3>
  <p class="hint">A text equivalent of the diagram above, for screen readers and quick scanning.</p>
  {#if graph.edges.length === 0}
    <p>No dependencies recorded.</p>
  {:else}
    <ul>
      {#each graph.edges as edge, i (i)}
        <li>
          <strong>{edge.from}</strong> depends on <strong>{edge.to}</strong>
          {#if nodeById(edge.to)}({nodeById(edge.to)?.status}){:else}(external reference){/if}
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .cycle-warning {
    background: var(--color-accent-soft);
    color: var(--color-danger);
    border-radius: 6px;
    padding: 0.5rem 0.75rem;
    font-size: 0.85rem;
  }
  .graph-scroll {
    overflow-x: auto;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: 1rem;
  }
  .graph {
    position: relative;
    display: flex;
    gap: 2.5rem;
    min-width: max-content;
  }
  .connectors {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    pointer-events: none;
    overflow: visible;
  }
  .connectors line {
    stroke: var(--color-border-strong);
    stroke-width: 1.5;
  }
  .connectors line.cyclic {
    stroke: var(--color-danger);
    stroke-dasharray: 4 3;
  }
  .column {
    display: grid;
    gap: 0.75rem;
    z-index: 1;
    min-width: 160px;
  }
  .node {
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: 0.5rem 0.6rem;
    background: var(--color-surface);
    display: grid;
    gap: 0.15rem;
    text-align: left;
    font: inherit;
    cursor: pointer;
  }
  .node-external {
    cursor: default;
    background: var(--color-surface-subtle);
    border-style: dashed;
  }
  .node.cyclic {
    border-color: var(--color-danger);
  }
  .node-id {
    font-size: 0.7rem;
    color: var(--color-text-muted);
  }
  .node-title {
    font-size: 0.85rem;
  }
  .node-state {
    font-size: 0.7rem;
    text-transform: capitalize;
  }
  .node-ready .node-state {
    color: var(--color-success);
  }
  .node-blocked .node-state {
    color: var(--color-danger);
  }
  .node-active .node-state {
    color: var(--color-accent);
  }
  .fallback {
    margin-top: 1rem;
  }
  .fallback h3 {
    font-size: 0.9rem;
    margin-bottom: 0.15rem;
  }
  .hint {
    color: var(--color-text-muted);
    font-size: 0.8rem;
    margin-top: 0;
  }
  .fallback ul {
    padding-left: 1.2rem;
    font-size: 0.85rem;
  }
</style>
