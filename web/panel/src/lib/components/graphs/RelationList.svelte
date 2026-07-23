<script lang="ts">
  import type { GraphEdge, GraphNode } from "./types";

  interface Props {
    nodes: GraphNode[];
    edges: GraphEdge[];
    caption: string;
    /** Show visibly instead of screen-reader-only. Defaults to hidden. */
    visible?: boolean;
  }
  let { nodes, edges, caption, visible = false }: Props = $props();

  const labelById = $derived(new Map(nodes.map((n) => [n.id, n.label])));

  function labelFor(id: string): string {
    return labelById.get(id) ?? id;
  }
</script>

<!--
  A node-and-edge canvas has no meaningful screen-reader semantics, so
  every graph view renders this plain enumeration of "A -> (relation
  type) -> B" for every edge currently loaded - the same bounded/focused
  subset the canvas is showing, never the full unbounded dataset.
-->
<table class:sr-only={!visible}>
  <caption>{caption}</caption>
  <thead>
    <tr>
      <th scope="col">From</th>
      <th scope="col">Relation</th>
      <th scope="col">To</th>
    </tr>
  </thead>
  <tbody>
    {#each edges as edge (edge.id)}
      <tr>
        <th scope="row">{labelFor(edge.source)}</th>
        <td>{edge.label ?? edge.type ?? "relates to"}</td>
        <td>{labelFor(edge.target)}</td>
      </tr>
    {/each}
  </tbody>
</table>
{#if edges.length === 0}
  <p class:sr-only={!visible}>No relations in the current view.</p>
{/if}

<style>
  table {
    border-collapse: collapse;
    font-size: 0.8rem;
    color: var(--color-text);
    margin-top: 0.5rem;
  }
  th,
  td {
    border: 1px solid var(--color-border);
    padding: 0.3rem 0.5rem;
    text-align: left;
  }
  thead th {
    background: var(--color-surface-subtle);
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
