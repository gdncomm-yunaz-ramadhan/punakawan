// Deterministic layered layout for the task dependency graph (§14.5's
// "Dependency View" MVP): CSS grid columns per layer, SVG connectors drawn
// by the caller from the coordinates this module hands back. No layout
// library - the graph is small enough (a workspace's own bd project, not
// a cross-org graph) that a longest-path layering pass is enough.

export interface Edge {
  from: string;
  to: string;
}

export interface LayoutResult {
  levels: Map<string, number>;
  maxLevel: number;
}

// layoutGraph assigns each node a level equal to the longest chain of
// dependencies below it (a node with no dependencies is level 0; a node
// that depends on a level-2 node is at least level 3). Edges pointing to a
// node outside nodeIds (bd's external:<project>:<capability> references)
// contribute one level without recursing into it. A node reached while
// still being visited (a cycle) is not re-entered - its contribution to
// the caller's level is treated as 0, which keeps the layout finite
// without hiding the cycle (Cycles is reported separately by the API and
// rendered as its own callout).
export function layoutGraph(nodeIds: string[], edges: Edge[]): LayoutResult {
  const idSet = new Set(nodeIds);
  const adjacency = new Map<string, string[]>();
  for (const id of nodeIds) adjacency.set(id, []);
  for (const e of edges) {
    if (idSet.has(e.from)) adjacency.get(e.from)?.push(e.to);
  }

  const levels = new Map<string, number>();
  const visiting = new Set<string>();

  function visit(node: string): number {
    const cached = levels.get(node);
    if (cached !== undefined) return cached;
    if (visiting.has(node)) return 0;
    visiting.add(node);

    let level = 0;
    for (const next of adjacency.get(node) ?? []) {
      if (!idSet.has(next)) {
        level = Math.max(level, 1);
        continue;
      }
      level = Math.max(level, visit(next) + 1);
    }

    visiting.delete(node);
    levels.set(node, level);
    return level;
  }

  for (const id of nodeIds) visit(id);

  let maxLevel = 0;
  for (const level of levels.values()) maxLevel = Math.max(maxLevel, level);
  return { levels, maxLevel };
}

// externalRefs lists every dependency target not present in nodeIds - bd's
// cross-project references - so the graph view can render them as
// External nodes per §14.5's node-state list, without fabricating an
// issue record for them.
export function externalRefs(nodeIds: string[], edges: Edge[]): string[] {
  const idSet = new Set(nodeIds);
  const seen = new Set<string>();
  for (const e of edges) {
    if (!idSet.has(e.to)) seen.add(e.to);
  }
  return [...seen];
}
