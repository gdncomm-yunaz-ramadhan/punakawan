import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import GraphCardHarness from "./fixtures/GraphCardHarness.svelte";

describe("GraphCard", () => {
  it("renders without crashing and shows its title", () => {
    render(GraphCardHarness, { props: { title: "Artifact relations" } });
    expect(screen.getByText("Artifact relations")).toBeTruthy();
  });

  it("renders provided slot content instead of the placeholder", () => {
    render(GraphCardHarness, { props: { withContent: true } });
    expect(screen.getByTestId("graph-card-slot-content")).toBeTruthy();
  });

  it("shows a placeholder message when no content is provided", () => {
    render(GraphCardHarness, { props: { withContent: false } });
    expect(screen.getByText("No graph content provided.")).toBeTruthy();
  });
});
