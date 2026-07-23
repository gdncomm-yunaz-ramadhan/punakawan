import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import RelationList from "../src/lib/components/graphs/RelationList.svelte";
import type { GraphEdge, GraphNode } from "../src/lib/components/graphs/types";

const nodes: GraphNode[] = [
  { id: "a", label: "Artifact A" },
  { id: "b", label: "Artifact B" },
  { id: "c", label: "Artifact C" },
];
const edges: GraphEdge[] = [
  { id: "e1", source: "a", target: "b", label: "derived from" },
  { id: "e2", source: "b", target: "c", type: "supersedes" },
];

describe("RelationList", () => {
  it("renders one row per edge with labeled source/relation/target", () => {
    render(RelationList, { props: { nodes, edges, caption: "Sample relations" } });

    const table = screen.getByText("Sample relations").closest("table");
    const rows = table?.querySelectorAll("tbody tr");
    expect(rows?.length).toBe(2);
    expect(rows?.[0].textContent).toContain("Artifact A");
    expect(rows?.[0].textContent).toContain("derived from");
    expect(rows?.[0].textContent).toContain("Artifact B");
    // second edge has no label, falls back to its type
    expect(rows?.[1].textContent).toContain("supersedes");
  });

  it("is screen-reader-only by default and visible when visible=true", () => {
    const { rerender } = render(RelationList, { props: { nodes, edges, caption: "Sample relations" } });
    const table = screen.getByText("Sample relations").closest("table") as HTMLTableElement;
    expect(table.className).toContain("sr-only");

    rerender({ nodes, edges, caption: "Sample relations", visible: true });
    expect(table.className).not.toContain("sr-only");
  });

  it("shows a 'no relations' message when edges is empty", () => {
    render(RelationList, { props: { nodes, edges: [], caption: "Empty relations", visible: true } });
    expect(screen.getByText("No relations in the current view.")).toBeTruthy();
  });
});
