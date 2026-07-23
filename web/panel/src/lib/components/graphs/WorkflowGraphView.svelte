<script lang="ts">
  import { untrack } from "svelte";
  import GraphCanvas, { type GraphCanvasApi } from "./GraphCanvas.svelte";
  import GraphControls from "./GraphControls.svelte";
  import RelationList from "./RelationList.svelte";
  import { boundedSubgraph } from "./hops";
  import { DEFAULT_NODE_CAP, type GraphEdge, type GraphLayoutName, type GraphNode } from "./types";

  export interface WorkflowGraphViewProps {
    /** Workflow run steps/tasks. */
    nodes: GraphNode[];
    /** Sequence/dependency edges between steps. */
    edges: GraphEdge[];
    title?: string;
    nodeCap?: number;
    /** Hops from a focus node to show initially, when the full set exceeds the cap. */
    focusHops?: number;
  }
  let {
    nodes,
    edges,
    title = "Workflow graph",
    nodeCap = DEFAULT_NODE_CAP,
    focusHops = 1,
  }: WorkflowGraphViewProps = $props();

  // Seeded once from the initial nodes/nodeCap - the focus node is meant
  // to be user-driven (selection/expand) after mount, not re-derived every
  // time the caller's props change shape.
  let focusNodeId: string | null = $state(
    untrack(() => (nodes.length > nodeCap ? nodes[0]?.id ?? null : null)),
  );
  let selectedNodeId: string | null = $state(null);
  let layoutName: GraphLayoutName = $state("breadthfirst");
  let api: GraphCanvasApi | null = $state(null);
  let expandedExtra = $state(new Set<string>());

  const visible = $derived.by(() => {
    if (!focusNodeId) return { nodes, edges };
    const bounded = boundedSubgraph(nodes, edges, focusNodeId, focusHops);
    if (expandedExtra.size === 0) return bounded;
    const extraIds = new Set(bounded.nodes.map((n) => n.id));
    for (const id of expandedExtra) extraIds.add(id);
    return {
      nodes: nodes.filter((n) => extraIds.has(n.id)),
      edges: edges.filter((e) => extraIds.has(e.source) && extraIds.has(e.target)),
    };
  });

  function handleExpand(nodeId: string) {
    const currentIds = new Set(visible.nodes.map((n) => n.id));
    for (const e of edges) {
      if (e.source === nodeId && !currentIds.has(e.target)) expandedExtra.add(e.target);
      if (e.target === nodeId && !currentIds.has(e.source)) expandedExtra.add(e.source);
    }
    expandedExtra = new Set(expandedExtra);
  }
</script>

<div class="graph-view">
  <GraphControls
    {api}
    {layoutName}
    onLayoutChange={(name) => (layoutName = name)}
    {selectedNodeId}
    onExpand={handleExpand}
  />
  <GraphCanvas
    nodes={visible.nodes}
    edges={visible.edges}
    ariaLabel={title}
    {nodeCap}
    {layoutName}
    focusNodeId={focusNodeId ?? undefined}
    onNodeSelect={(id) => (selectedNodeId = id)}
    onNodeExpand={handleExpand}
    onReady={(instance) => (api = instance)}
  />
  <RelationList
    nodes={visible.nodes}
    edges={visible.edges}
    caption={`${title}: sequence/dependency relations in the current view.`}
  />
</div>

<style>
  .graph-view {
    display: grid;
    gap: 0.5rem;
  }
</style>
