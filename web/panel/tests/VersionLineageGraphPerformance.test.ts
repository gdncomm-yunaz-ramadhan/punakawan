// Chart/graph performance coverage for apy.7's hardening pass, exercising
// VersionLineageGraphView (the lineage graph reused by ProposalReview -
// see that component's "Version lineage" section) against a synthetic
// large lineage: 60 sequential proposal attempts, well past the default
// 150-node cap's usual trigger point for a *linear* chain but easily
// past what any real review is expected to accumulate, plus enough to
// exercise the existing boundedSubgraph/nodeCap bounding logic
// (src/lib/components/graphs/hops.ts) rather than adding new perf
// machinery. No existing perf test pattern for graphs exists elsewhere
// in this repo (checked tests/hops.test.ts, tests/GraphCanvas.test.ts,
// tests/VersionLineageGraphView.test.ts) - this establishes one scoped
// to "renders without hanging, and the bounding logic actually reduces
// what's sent to Cytoscape" rather than measuring wall-clock frame time,
// which jsdom (no real rendering/compositing) can't meaningfully report.
import { render, screen, waitFor } from "@testing-library/svelte";
import { describe, expect, it, vi } from "vitest";
import VersionLineageGraphView from "../src/lib/components/graphs/VersionLineageGraphView.svelte";
import type { GraphEdge, GraphNode } from "../src/lib/components/graphs/types";

let lastElementCount = 0;
let constructedCount = 0;

vi.mock("cytoscape", () => {
  const factory = vi.fn((opts: { elements?: unknown[] }) => {
    constructedCount++;
    lastElementCount = opts.elements?.length ?? 0;
    return {
      on: vi.fn(),
      destroy: vi.fn(),
      style: () => ({ update: vi.fn() }),
      zoom: vi.fn(() => 1),
      fit: vi.fn(),
      layout: () => ({ run: vi.fn() }),
    };
  });
  return { default: factory };
});

// A linear chain: base -> attempt-1 -> attempt-2 -> ... -> attempt-N,
// mirroring ProposalReview's own lineageNodes/lineageEdges derivation for
// a review that went through many "request changes" rounds.
function buildLinearLineage(attempts: number): { nodes: GraphNode[]; edges: GraphEdge[] } {
  const nodes: GraphNode[] = [{ id: "base", label: "v1 (base)", type: "version" }];
  const edges: GraphEdge[] = [];
  for (let i = 1; i <= attempts; i++) {
    nodes.push({ id: `attempt-${i}`, label: `Attempt ${i}`, type: "proposal" });
    const source = i === 1 ? "base" : `attempt-${i - 1}`;
    edges.push({ id: `edge-${i}`, source, target: `attempt-${i}`, label: "revised" });
  }
  return { nodes, edges };
}

describe("VersionLineageGraphView performance (synthetic large lineage)", () => {
  it("renders a 60-attempt lineage without hanging, within a generous time budget", async () => {
    const { nodes, edges } = buildLinearLineage(60);
    lastElementCount = 0;
    constructedCount = 0;

    const start = performance.now();
    render(VersionLineageGraphView, { props: { nodes, edges, nodeCap: 150, focusHops: 3 } });
    await waitFor(() => expect(constructedCount).toBe(1));
    const elapsed = performance.now() - start;

    // Generous budget: this is a correctness/hang guard, not a strict
    // frame-time SLA (jsdom can't report real paint/compositing cost).
    expect(elapsed).toBeLessThan(2000);
  });

  it("bounds the rendered subgraph via focusHops rather than passing all 61 nodes to Cytoscape", async () => {
    const { nodes, edges } = buildLinearLineage(60);
    lastElementCount = 0;
    constructedCount = 0;

    // 61 total nodes (base + 60 attempts) exceeds a small nodeCap, so
    // VersionLineageGraphView's own focus-node seeding (see its
    // `untrack(() => nodes.length > nodeCap ? nodes[0]?.id ...)`) picks
    // the first node as focus and boundedSubgraph restricts to focusHops
    // steps from it - exercising the same bounding logic a real
    // 50+-attempt review would trigger, per the plan's "cap or
    // progressively expand visible nodes" requirement.
    render(VersionLineageGraphView, { props: { nodes, edges, nodeCap: 10, focusHops: 3 } });
    await waitFor(() => expect(constructedCount).toBe(1));

    // focusHops=3 from "base" in a linear chain reaches base + 3 attempts
    // = 4 nodes, well under the full 61 - elements sent to Cytoscape are
    // nodes + edges among them (3 edges for a 4-node linear run).
    expect(lastElementCount).toBeLessThan(nodes.length + edges.length);
    expect(lastElementCount).toBe(4 + 3);
  });

  it("still renders (unbounded) when the full lineage is under the node cap", async () => {
    const { nodes, edges } = buildLinearLineage(5);
    lastElementCount = 0;
    constructedCount = 0;

    render(VersionLineageGraphView, { props: { nodes, edges, nodeCap: 150 } });
    await waitFor(() => expect(constructedCount).toBe(1));

    // 6 nodes + 5 edges, all included since 6 <= nodeCap.
    expect(lastElementCount).toBe(6 + 5);
  });

  it("exposes an accessible relation-list fallback even for the large lineage", async () => {
    const { nodes, edges } = buildLinearLineage(60);
    render(VersionLineageGraphView, { props: { nodes, edges, nodeCap: 150, focusHops: 3 } });

    await waitFor(() => expect(constructedCount).toBeGreaterThan(0));
    await waitFor(() => expect(screen.getByLabelText("Version lineage")).toBeTruthy());
    expect(screen.getByText(/Version lineage:/)).toBeTruthy();
  });
});
