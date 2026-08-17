import { render, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import GraphCanvas from "../src/lib/components/graphs/GraphCanvas.svelte";
import type { GraphEdge, GraphNode } from "../src/lib/components/graphs/types";

let constructedCount = 0;
let lastStyle: unknown = null;
const styleUpdate = vi.fn();
const destroy = vi.fn();
const layoutRun = vi.fn();
const zoomFn = vi.fn((v?: number) => (v === undefined ? 1 : undefined));
const fitFn = vi.fn();
const onFn = vi.fn();
const addFn = vi.fn();
const removeFn = vi.fn();

// Minimal in-memory element set standing in for the real Cytoscape core's
// element store, just enough to exercise GraphCanvas's add/remove/
// getElementById-based diffing without a real canvas/WebGL context.
interface FakeElement {
  id: string;
  isNode: boolean;
}
let liveElements: FakeElement[] = [];

function elementIdOf(el: { data: { id?: string; source?: string } }): string {
  return el.data.id ?? "";
}

function isNodeDef(el: { data: { source?: string } }): boolean {
  return el.data.source === undefined;
}

function makeCollection(items: FakeElement[]) {
  return {
    length: items.length,
    empty: () => items.length === 0,
    // Not a real Cytoscape API - internal plumbing so the mock's remove()
    // can recover which ids a previously-filtered collection stands for.
    ids: () => items.map((it) => it.id),
    filter: (fn: (ele: { id: () => string }) => boolean) =>
      makeCollection(items.filter((it) => fn({ id: () => it.id }))),
    nodes: () => makeCollection(items.filter((it) => it.isNode)),
    addClass: vi.fn(),
    removeClass: vi.fn(),
    layout: () => ({ run: layoutRun }),
  };
}

// Mocking cytoscape itself (rather than fighting jsdom's lack of a real
// canvas/WebGL context) - the standard/reliable approach for
// Cytoscape.js-consuming component tests per the task's test strategy.
vi.mock("cytoscape", () => {
  const factory = vi.fn((opts: { elements?: { data: { id?: string; source?: string } }[] }) => {
    constructedCount++;
    liveElements = (opts.elements ?? []).map((el) => ({ id: elementIdOf(el), isNode: isNodeDef(el) }));
    return {
      on: onFn,
      destroy,
      style: (s: unknown) => {
        lastStyle = s;
        return { update: styleUpdate };
      },
      zoom: zoomFn,
      fit: fitFn,
      layout: () => ({ run: layoutRun }),
      elements: () => makeCollection(liveElements),
      getElementById: (id: string) => makeCollection(liveElements.filter((it) => it.id === id)),
      collection: () => makeCollection([]),
      add: addFn.mockImplementation((els: { data: { id?: string; source?: string } } | { data: { id?: string; source?: string } }[]) => {
        const arr = Array.isArray(els) ? els : [els];
        const added = arr.map((el) => ({ id: elementIdOf(el), isNode: isNodeDef(el) }));
        liveElements.push(...added);
        return makeCollection(added);
      }),
      remove: removeFn.mockImplementation((coll: { ids?: () => string[] }) => {
        const removedIds = new Set(coll.ids?.() ?? []);
        liveElements = liveElements.filter((it) => !removedIds.has(it.id));
      }),
    };
  });
  return { default: factory };
});

beforeEach(() => {
  constructedCount = 0;
  lastStyle = null;
  liveElements = [];
  styleUpdate.mockClear();
  destroy.mockClear();
  layoutRun.mockClear();
  zoomFn.mockClear();
  fitFn.mockClear();
  onFn.mockClear();
  addFn.mockClear();
  removeFn.mockClear();
  document.documentElement.removeAttribute("data-theme");
});

const nodes: GraphNode[] = [
  { id: "a", label: "A" },
  { id: "b", label: "B" },
  { id: "c", label: "C" },
];
const edges: GraphEdge[] = [
  { id: "e1", source: "a", target: "b", label: "depends on" },
  { id: "e2", source: "b", target: "c", label: "depends on" },
];

function manyNodes(count: number): GraphNode[] {
  return Array.from({ length: count }, (_, i) => ({ id: `n${i}`, label: `Node ${i}` }));
}

describe("GraphCanvas", () => {
  it("renders a normal canvas and constructs Cytoscape when under the node cap", async () => {
    render(GraphCanvas, { props: { nodes, edges, ariaLabel: "Sample graph" } });
    await waitFor(() => expect(constructedCount).toBe(1));
  });

  it("shows the capped 'too large' state and never constructs Cytoscape when over 150 nodes", async () => {
    const { getByRole } = render(GraphCanvas, {
      props: { nodes: manyNodes(151), edges: [], ariaLabel: "Huge graph" },
    });
    await waitFor(() => expect(getByRole("status")).toBeTruthy());
    expect(constructedCount).toBe(0);
    expect(getByRole("status").textContent).toContain("151 nodes");
    expect(getByRole("status").textContent).toContain("150-node display limit");
  });

  it("respects a custom nodeCap prop", async () => {
    const { getByRole } = render(GraphCanvas, {
      props: { nodes: manyNodes(10), edges: [], ariaLabel: "Small cap graph", nodeCap: 5 },
    });
    await waitFor(() => expect(getByRole("status")).toBeTruthy());
    expect(constructedCount).toBe(0);
  });

  it("recolors (calls style().update()) on a theme toggle without reconstructing the instance", async () => {
    render(GraphCanvas, { props: { nodes, edges, ariaLabel: "Sample graph" } });
    await waitFor(() => expect(constructedCount).toBe(1));

    document.documentElement.setAttribute("data-theme", "dark");
    await waitFor(() => expect(styleUpdate).toHaveBeenCalled());
    expect(constructedCount).toBe(1);
    expect(lastStyle).toBeTruthy();
  });

  it("disables layout animation when reduced motion is requested", async () => {
    const matchMediaSpy = vi.spyOn(window, "matchMedia").mockImplementation(
      (query: string) =>
        ({
          matches: query.includes("prefers-reduced-motion"),
          media: query,
          addEventListener: vi.fn(),
          removeEventListener: vi.fn(),
        }) as unknown as MediaQueryList,
    );

    render(GraphCanvas, { props: { nodes, edges, ariaLabel: "Sample graph" } });
    await waitFor(() => expect(constructedCount).toBe(1));

    const factoryCall = (await import("cytoscape")).default as unknown as ReturnType<typeof vi.fn>;
    const lastCall = factoryCall.mock.calls[factoryCall.mock.calls.length - 1];
    const options = lastCall[0] as { layout: { animate: boolean; animationDuration: number } };
    expect(options.layout.animate).toBe(false);
    expect(options.layout.animationDuration).toBe(0);

    matchMediaSpy.mockRestore();
  });

  it("updates elements in place rather than destroying the instance when the new nodes overlap the current set", async () => {
    const { rerender } = render(GraphCanvas, { props: { nodes, edges, ariaLabel: "Sample graph" } });
    await waitFor(() => expect(constructedCount).toBe(1));

    const grownNodes: GraphNode[] = [...nodes, { id: "d", label: "D" }];
    const grownEdges: GraphEdge[] = [...edges, { id: "e3", source: "c", target: "d", label: "depends on" }];
    await rerender({ nodes: grownNodes, edges: grownEdges, ariaLabel: "Sample graph" });

    // Same underlying graph plus one new node/edge - this must be an
    // incremental add, not a teardown-and-rebuild.
    expect(constructedCount).toBe(1);
    expect(destroy).not.toHaveBeenCalled();
    expect(addFn).toHaveBeenCalled();
    // Only the newly-added node gets laid out, not the whole graph.
    expect(layoutRun).toHaveBeenCalled();

    const shrunkNodes: GraphNode[] = nodes.slice(0, 2);
    const shrunkEdges: GraphEdge[] = [edges[0]];
    await rerender({ nodes: shrunkNodes, edges: shrunkEdges, ariaLabel: "Sample graph" });

    // Same graph, narrower bounds - still an incremental removal, not a
    // reconstruction, and the still-visible nodes never get removed.
    expect(constructedCount).toBe(1);
    expect(destroy).not.toHaveBeenCalled();
    expect(removeFn).toHaveBeenCalled();
  });

  it("falls back to a full reconstruct when the new node set shares nothing with the current one", async () => {
    const { rerender } = render(GraphCanvas, { props: { nodes, edges, ariaLabel: "Sample graph" } });
    await waitFor(() => expect(constructedCount).toBe(1));

    // A wholesale replacement can't be diffed sensibly - swapping to a
    // disjoint id space should still warrant a real teardown+rebuild.
    const disjointNodes: GraphNode[] = [
      { id: "x", label: "X" },
      { id: "y", label: "Y" },
    ];
    await rerender({ nodes: disjointNodes, edges: [], ariaLabel: "Sample graph" });

    await waitFor(() => expect(constructedCount).toBe(2));
    expect(destroy).toHaveBeenCalled();
  });
});
