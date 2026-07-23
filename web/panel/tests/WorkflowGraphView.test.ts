import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import WorkflowGraphView from "../src/lib/components/graphs/WorkflowGraphView.svelte";
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
  { id: "start", label: "Start", type: "step" },
  { id: "review", label: "Review", type: "step" },
  { id: "merge", label: "Merge", type: "gate" },
];
const edges: GraphEdge[] = [
  { id: "e1", source: "start", target: "review", label: "then" },
  { id: "e2", source: "review", target: "merge", label: "then" },
];

describe("WorkflowGraphView", () => {
  it("renders without crashing given sample workflow steps", async () => {
    render(WorkflowGraphView, { props: { nodes, edges } });
    await waitFor(() => expect(constructedCount).toBe(1));
    expect(screen.getByLabelText("Workflow graph")).toBeTruthy();
  });

  it("renders the relation-list fallback with one row per edge in view", async () => {
    render(WorkflowGraphView, { props: { nodes, edges } });
    await waitFor(() => expect(constructedCount).toBe(1));

    const table = screen.getByText(/Workflow graph:/).closest("table");
    expect(table?.querySelectorAll("tbody tr").length).toBe(2);
  });

  it("defaults to the breadthfirst layout, appropriate for sequential steps", () => {
    render(WorkflowGraphView, { props: { nodes, edges } });
    expect((screen.getByRole("combobox", { name: "Layout" }) as HTMLSelectElement).value).toBe("breadthfirst");
  });
});
