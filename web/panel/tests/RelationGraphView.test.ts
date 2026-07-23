import { render, screen, waitFor } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import RelationGraphView from "../src/lib/components/graphs/RelationGraphView.svelte";
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
  { id: "k1", label: "Knowledge Record 1", type: "artifact" },
  { id: "k2", label: "Knowledge Record 2", type: "artifact" },
];
const edges: GraphEdge[] = [{ id: "e1", source: "k1", target: "k2", type: "supersedes" }];

describe("RelationGraphView", () => {
  it("renders without crashing given sample records and typed relations", async () => {
    render(RelationGraphView, { props: { nodes, edges } });
    await waitFor(() => expect(constructedCount).toBe(1));
    expect(screen.getByLabelText("Relation graph")).toBeTruthy();
  });

  it("renders the relation-list fallback enumerating the typed relation", async () => {
    render(RelationGraphView, { props: { nodes, edges } });
    await waitFor(() => expect(constructedCount).toBe(1));

    const table = screen.getByText(/Relation graph:/).closest("table");
    expect(table?.querySelectorAll("tbody tr").length).toBe(1);
    expect(table?.textContent).toContain("supersedes");
  });
});
