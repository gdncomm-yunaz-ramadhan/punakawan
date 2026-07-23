import type { GraphEdge, GraphNode } from "./types";

// Computes the bounded, focused subset of nodes/edges within `hops` steps
// of `focusId` (undirected - either endpoint counts as adjacent). This is
// the "focused-subgraph-loading" behavior: initial render shows only
// nodes within N hops of a designated focus node (or everything, if the
// full set is already under the node cap - callers decide that upstream).
export function boundedSubgraph(
  nodes: GraphNode[],
  edges: GraphEdge[],
  focusId: string,
  hops: number,
): { nodes: GraphNode[]; edges: GraphEdge[] } {
  const byId = new Map(nodes.map((n) => [n.id, n]));
  if (!byId.has(focusId)) return { nodes: [], edges: [] };

  const adjacency = new Map<string, Set<string>>();
  for (const e of edges) {
    if (!adjacency.has(e.source)) adjacency.set(e.source, new Set());
    if (!adjacency.has(e.target)) adjacency.set(e.target, new Set());
    adjacency.get(e.source)?.add(e.target);
    adjacency.get(e.target)?.add(e.source);
  }

  const visited = new Set<string>([focusId]);
  let frontier = [focusId];
  for (let hop = 0; hop < hops; hop++) {
    const next: string[] = [];
    for (const id of frontier) {
      for (const neighbor of adjacency.get(id) ?? []) {
        if (!visited.has(neighbor)) {
          visited.add(neighbor);
          next.push(neighbor);
        }
      }
    }
    frontier = next;
    if (frontier.length === 0) break;
  }

  const boundedNodes = nodes.filter((n) => visited.has(n.id));
  const boundedEdges = edges.filter((e) => visited.has(e.source) && visited.has(e.target));
  return { nodes: boundedNodes, edges: boundedEdges };
}

// Returns the immediate (1-hop) neighbor ids of `nodeId` that are not
// already in `currentIds` - used by the "Expand" action to pull in the
// next hop around a node without recomputing the whole bounded subgraph.
export function nextHopNodeIds(edges: GraphEdge[], nodeId: string, currentIds: Set<string>): string[] {
  const found = new Set<string>();
  for (const e of edges) {
    if (e.source === nodeId && !currentIds.has(e.target)) found.add(e.target);
    if (e.target === nodeId && !currentIds.has(e.source)) found.add(e.source);
  }
  return [...found];
}
