import { fireEvent, render, waitFor } from "@testing-library/svelte";
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

// Stand-in for whatever a node's rendered dimensions would resolve to
// (in real Cytoscape, graphStyle.ts's `width: "label"` sizing) before any
// manual resize - arbitrary but fixed, so the resize math in the tests
// below has a known starting point to assert deltas against.
const DEFAULT_NODE_WIDTH = 100;
const DEFAULT_NODE_HEIGHT = 50;

// Minimal in-memory element set standing in for the real Cytoscape core's
// element store, just enough to exercise GraphCanvas's add/remove/
// getElementById-based diffing without a real canvas/WebGL context.
interface FakeElement {
  id: string;
  isNode: boolean;
  data: Record<string, unknown>;
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
    id: () => items[0]?.id,
    // Real Cytoscape's getter form reads the first element in the
    // collection; GraphCanvas only ever calls these on the single-element
    // collections getElementById() returns, so that's all this needs to
    // support.
    data: (arg?: string | Record<string, unknown>, value?: unknown) => {
      const el = items[0];
      if (!el) return undefined;
      if (arg === undefined) return el.data;
      if (typeof arg === "object") {
        Object.assign(el.data, arg);
        return makeCollection(items);
      }
      if (value === undefined) return el.data[arg];
      el.data[arg] = value;
      return makeCollection(items);
    },
    width: () => (items[0]?.data.width as number | undefined) ?? DEFAULT_NODE_WIDTH,
    height: () => (items[0]?.data.height as number | undefined) ?? DEFAULT_NODE_HEIGHT,
    renderedWidth: () => (items[0]?.data.width as number | undefined) ?? DEFAULT_NODE_WIDTH,
    renderedHeight: () => (items[0]?.data.height as number | undefined) ?? DEFAULT_NODE_HEIGHT,
    renderedPosition: () => ({ x: 100, y: 100 }),
  };
}

// Mocking cytoscape itself (rather than fighting jsdom's lack of a real
// canvas/WebGL context) - the standard/reliable approach for
// Cytoscape.js-consuming component tests per the task's test strategy.
vi.mock("cytoscape", () => {
  const factory = vi.fn((opts: { elements?: { data: { id?: string; source?: string } }[] }) => {
    constructedCount++;
    liveElements = (opts.elements ?? []).map((el) => ({
      id: elementIdOf(el),
      isNode: isNodeDef(el),
      data: { ...el.data },
    }));
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
        const added = arr.map((el) => ({ id: elementIdOf(el), isNode: isNodeDef(el), data: { ...el.data } }));
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

// Pulls out the handler passed to a specific cy.on(...) registration -
// the mock's `on` is a plain spy, not a real dispatcher, so tests that
// need to simulate a Cytoscape-fired event (tap, remove, viewport change)
// invoke the captured handler directly instead.
function onHandler(event: string, selector?: string): (evt: unknown) => void {
  const call = onFn.mock.calls.find((c) =>
    selector === undefined ? c[0] === event && c.length === 2 : c[0] === event && c[1] === selector,
  );
  if (!call) throw new Error(`no cy.on("${event}"${selector ? `, "${selector}"` : ""}, ...) registration found`);
  return call[call.length - 1] as (evt: unknown) => void;
}

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

  describe("node resize handle", () => {
    // jsdom doesn't implement PointerEvent in every version - MouseEvent
    // carries the same clientX/clientY fields our handlers read, and
    // window.addEventListener("pointermove", ...) dispatches on `type`
    // alone, so it's an equally valid stand-in here.
    const PointerEventCtor: typeof MouseEvent =
      typeof PointerEvent !== "undefined" ? (PointerEvent as unknown as typeof MouseEvent) : MouseEvent;

    function dispatchOnWindow(type: string, init: MouseEventInit = {}) {
      window.dispatchEvent(new PointerEventCtor(type, { bubbles: true, ...init }));
    }

    // fireEvent.pointerDown's built-in event construction doesn't carry
    // clientX/clientY through in this jsdom (no native PointerEvent to
    // build on) - dispatch the same MouseEvent-based stand-in directly on
    // the handle instead, so beginResize sees real starting coordinates.
    function dispatchOnElement(el: Element, type: string, init: MouseEventInit = {}) {
      el.dispatchEvent(new PointerEventCtor(type, { bubbles: true, ...init }));
    }

    it("shows a resize handle only once a node is selected, and hides it again on a background tap", async () => {
      const { queryByRole } = render(GraphCanvas, { props: { nodes, edges, ariaLabel: "Sample graph" } });
      await waitFor(() => expect(constructedCount).toBe(1));

      expect(queryByRole("button", { name: "Resize node" })).toBeNull();

      onHandler("tap", "node")({ target: { id: () => "a" } });
      await waitFor(() => expect(queryByRole("button", { name: "Resize node" })).toBeTruthy());

      // A tap whose target is the core instance itself (not an element)
      // is Cytoscape's own idiom for "the user tapped empty canvas" -
      // recover the real instance the mocked factory returned so the
      // component's `evt.target === cy` check actually matches.
      const factoryCall = (await import("cytoscape")).default as unknown as ReturnType<typeof vi.fn>;
      const instance = factoryCall.mock.results[factoryCall.mock.results.length - 1].value;
      onHandler("tap")({ target: instance });
      await waitFor(() => expect(queryByRole("button", { name: "Resize node" })).toBeNull());
    });

    it("drag-resizing the handle writes clamped width/height onto the selected node's data, leaving other nodes untouched", async () => {
      const { getByRole } = render(GraphCanvas, { props: { nodes, edges, ariaLabel: "Sample graph" } });
      await waitFor(() => expect(constructedCount).toBe(1));

      onHandler("tap", "node")({ target: { id: () => "a" } });
      const handle = await waitFor(() => getByRole("button", { name: "Resize node" }));

      dispatchOnElement(handle, "pointerdown", { clientX: 0, clientY: 0 });
      dispatchOnWindow("pointermove", { clientX: 40, clientY: 20 });
      dispatchOnWindow("pointerup");

      const nodeA = liveElements.find((el) => el.id === "a");
      // Started at the DEFAULT_NODE_WIDTH/HEIGHT stand-in, moved by
      // (40, 20) screen px at zoom 1 (the mock's zoom() returns 1).
      expect(nodeA?.data.width).toBe(DEFAULT_NODE_WIDTH + 40);
      expect(nodeA?.data.height).toBe(DEFAULT_NODE_HEIGHT + 20);

      // Every other node/edge is untouched - a per-node data write, not a
      // graph-wide operation.
      const nodeB = liveElements.find((el) => el.id === "b");
      const nodeC = liveElements.find((el) => el.id === "c");
      expect(nodeB?.data.width).toBeUndefined();
      expect(nodeC?.data.width).toBeUndefined();

      // No rebuild, no add/remove of elements - this is exactly the
      // "per-node data change, not grounds for a full graph rebuild"
      // behavior punokawan-z8dp's diffing depends on.
      expect(destroy).not.toHaveBeenCalled();
      expect(addFn).not.toHaveBeenCalled();
      expect(removeFn).not.toHaveBeenCalled();
      expect(constructedCount).toBe(1);
    });

    it("clamps drag-resize to the configured min/max bounds", async () => {
      const { getByRole } = render(GraphCanvas, { props: { nodes, edges, ariaLabel: "Sample graph" } });
      await waitFor(() => expect(constructedCount).toBe(1));

      onHandler("tap", "node")({ target: { id: () => "a" } });
      const handle = await waitFor(() => getByRole("button", { name: "Resize node" }));

      // Drag far past the upper bound in both dimensions.
      dispatchOnElement(handle, "pointerdown", { clientX: 0, clientY: 0 });
      dispatchOnWindow("pointermove", { clientX: 100000, clientY: 100000 });
      dispatchOnWindow("pointerup");

      let nodeA = liveElements.find((el) => el.id === "a");
      expect(nodeA?.data.width).toBe(480);
      expect(nodeA?.data.height).toBe(320);

      // Drag far past the lower bound in both dimensions.
      dispatchOnElement(handle, "pointerdown", { clientX: 0, clientY: 0 });
      dispatchOnWindow("pointermove", { clientX: -100000, clientY: -100000 });
      dispatchOnWindow("pointerup");

      nodeA = liveElements.find((el) => el.id === "a");
      expect(nodeA?.data.width).toBe(60);
      expect(nodeA?.data.height).toBe(40);
    });

    it("resizes via arrow keys while the handle has focus", async () => {
      const { getByRole } = render(GraphCanvas, { props: { nodes, edges, ariaLabel: "Sample graph" } });
      await waitFor(() => expect(constructedCount).toBe(1));

      onHandler("tap", "node")({ target: { id: () => "a" } });
      const handle = await waitFor(() => getByRole("button", { name: "Resize node" }));

      await fireEvent.keyDown(handle, { key: "ArrowRight" });
      await fireEvent.keyDown(handle, { key: "ArrowDown" });

      const nodeA = liveElements.find((el) => el.id === "a");
      expect(nodeA?.data.width).toBe(DEFAULT_NODE_WIDTH + 12);
      expect(nodeA?.data.height).toBe(DEFAULT_NODE_HEIGHT + 12);
      expect(destroy).not.toHaveBeenCalled();
    });

    it("drops the handle if the resized node is removed from the visible set", async () => {
      const { getByRole, queryByRole } = render(GraphCanvas, {
        props: { nodes, edges, ariaLabel: "Sample graph" },
      });
      await waitFor(() => expect(constructedCount).toBe(1));

      onHandler("tap", "node")({ target: { id: () => "a" } });
      await waitFor(() => expect(getByRole("button", { name: "Resize node" })).toBeTruthy());

      // Simulate diffAndUpdate's cy.remove(...) pruning node "a" out of
      // the bounded/focused subgraph - GraphCanvas's cy.on("remove",
      // "node", ...) handler should notice and drop the dangling handle.
      onHandler("remove", "node")({ target: { id: () => "a" } });

      await waitFor(() => expect(queryByRole("button", { name: "Resize node" })).toBeNull());
    });
  });
});
