import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import VersionLineageGraphView from "../src/lib/components/graphs/VersionLineageGraphView.svelte";
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
  { id: "v1", label: "v1", type: "version" },
  { id: "v2", label: "v2", type: "version" },
  { id: "v3", label: "v3", type: "version" },
];
const edges: GraphEdge[] = [
  { id: "e1", source: "v2", target: "v1", label: "derived from" },
  { id: "e2", source: "v3", target: "v2", label: "derived from" },
];

describe("VersionLineageGraphView", () => {
  it("renders without crashing given a sample linear lineage", async () => {
    render(VersionLineageGraphView, { props: { nodes, edges } });
    await waitFor(() => expect(constructedCount).toBe(1));
    expect(screen.getByLabelText("Version lineage")).toBeTruthy();
  });

  it("renders the relation-list fallback with one row per derived-from edge", async () => {
    render(VersionLineageGraphView, { props: { nodes, edges } });
    await waitFor(() => expect(constructedCount).toBe(1));

    const table = screen.getByText(/Version lineage:/).closest("table");
    expect(table?.querySelectorAll("tbody tr").length).toBe(2);
  });

  it("defaults to the breadthfirst layout, appropriate for a mostly-linear/tree lineage", () => {
    render(VersionLineageGraphView, { props: { nodes, edges } });
    expect((screen.getByRole("combobox", { name: "Layout" }) as HTMLSelectElement).value).toBe("breadthfirst");
  });
});
