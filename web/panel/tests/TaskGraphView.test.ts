import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import TaskGraphView from "../src/lib/components/graphs/TaskGraphView.svelte";
import type { GraphEdge, GraphNode } from "../src/lib/components/graphs/types";

let constructedCount = 0;

vi.mock("cytoscape", () => {
  const factory = vi.fn((_opts: unknown) => {
    constructedCount++;
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

beforeEach(() => {
  constructedCount = 0;
});

const nodes: GraphNode[] = [
  { id: "t1", label: "Task 1", type: "task" },
  { id: "t2", label: "Task 2", type: "task" },
  { id: "t3", label: "Task 3", type: "task" },
];
const edges: GraphEdge[] = [
  { id: "e1", source: "t1", target: "t2", type: "dependency" },
  { id: "e2", source: "t2", target: "t3", type: "dependency" },
];

function manyNodes(count: number): GraphNode[] {
  return Array.from({ length: count }, (_, i) => ({ id: `t${i}`, label: `Task ${i}`, type: "task" }));
}

describe("TaskGraphView", () => {
  it("renders without crashing given sample tasks", async () => {
    render(TaskGraphView, { props: { nodes, edges } });
    await waitFor(() => expect(constructedCount).toBe(1));
    expect(screen.getByLabelText("Task dependency graph")).toBeTruthy();
  });

  it("renders the relation-list fallback with one row per dependency edge", async () => {
    render(TaskGraphView, { props: { nodes, edges } });
    await waitFor(() => expect(constructedCount).toBe(1));

    const table = screen.getByText(/Task dependency graph:/).closest("table");
    expect(table?.querySelectorAll("tbody tr").length).toBe(2);
  });

  it("shows the capped state and a bounded relation list when over the node cap", async () => {
    const big = manyNodes(200);
    const chainEdges: GraphEdge[] = big.slice(0, -1).map((n, i) => ({
      id: `e${i}`,
      source: n.id,
      target: big[i + 1].id,
      type: "dependency",
    }));
    const { getByRole } = render(TaskGraphView, { props: { nodes: big, edges: chainEdges } });

    await waitFor(() => expect(getByRole("status")).toBeTruthy());
    expect(constructedCount).toBe(0);

    // Relation list still exists and is bounded (far fewer than 199 edges),
    // since a focus node + focusHops was chosen automatically.
    const table = getByRole("status").closest(".graph-view")?.querySelector("table");
    const rowCount = table?.querySelectorAll("tbody tr").length ?? 0;
    expect(rowCount).toBeLessThan(199);
  });
});
