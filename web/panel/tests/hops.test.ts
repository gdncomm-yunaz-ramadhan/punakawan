import { describe, expect, it } from "vitest";
import { boundedSubgraph, nextHopNodeIds } from "../src/lib/components/graphs/hops";
import type { GraphEdge, GraphNode } from "../src/lib/components/graphs/types";

// A -- B -- C -- D  (chain), plus an isolated node E.
const nodes: GraphNode[] = [
  { id: "A", label: "A" },
  { id: "B", label: "B" },
  { id: "C", label: "C" },
  { id: "D", label: "D" },
  { id: "E", label: "E" },
];
const edges: GraphEdge[] = [
  { id: "e1", source: "A", target: "B" },
  { id: "e2", source: "B", target: "C" },
  { id: "e3", source: "C", target: "D" },
];

describe("boundedSubgraph", () => {
  it("includes only the focus node at 0 hops", () => {
    const { nodes: n, edges: e } = boundedSubgraph(nodes, edges, "B", 0);
    expect(n.map((x) => x.id)).toEqual(["B"]);
    expect(e).toHaveLength(0);
  });

  it("includes immediate neighbors at 1 hop", () => {
    const { nodes: n } = boundedSubgraph(nodes, edges, "B", 1);
    expect(new Set(n.map((x) => x.id))).toEqual(new Set(["A", "B", "C"]));
  });

  it("includes 2-hop neighbors and their connecting edges", () => {
    const { nodes: n, edges: e } = boundedSubgraph(nodes, edges, "B", 2);
    expect(new Set(n.map((x) => x.id))).toEqual(new Set(["A", "B", "C", "D"]));
    expect(e).toHaveLength(3);
  });

  it("never includes an isolated node unless it is the focus itself", () => {
    const { nodes: n } = boundedSubgraph(nodes, edges, "B", 10);
    expect(n.map((x) => x.id)).not.toContain("E");
  });

  it("returns empty when the focus id does not exist", () => {
    const { nodes: n, edges: e } = boundedSubgraph(nodes, edges, "missing", 2);
    expect(n).toHaveLength(0);
    expect(e).toHaveLength(0);
  });
});

describe("nextHopNodeIds", () => {
  it("returns the immediate neighbors not already present", () => {
    const current = new Set(["B"]);
    const next = nextHopNodeIds(edges, "B", current);
    expect(new Set(next)).toEqual(new Set(["A", "C"]));
  });

  it("excludes neighbors already in the current set", () => {
    const current = new Set(["B", "C"]);
    const next = nextHopNodeIds(edges, "B", current);
    expect(next).toEqual(["A"]);
  });
});
