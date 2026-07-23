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

// Mocking cytoscape itself (rather than fighting jsdom's lack of a real
// canvas/WebGL context) - the standard/reliable approach for
// Cytoscape.js-consuming component tests per the task's test strategy.
vi.mock("cytoscape", () => {
  const factory = vi.fn((_opts: unknown) => {
    constructedCount++;
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
    };
  });
  return { default: factory };
});

beforeEach(() => {
  constructedCount = 0;
  lastStyle = null;
  styleUpdate.mockClear();
  destroy.mockClear();
  layoutRun.mockClear();
  zoomFn.mockClear();
  fitFn.mockClear();
  onFn.mockClear();
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
});
