import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import ChartCardHarness from "./fixtures/ChartCardHarness.svelte";

describe("ChartCard", () => {
  it("renders without crashing and shows its title", () => {
    render(ChartCardHarness, { props: { title: "Review activity trend" } });
    expect(screen.getByText("Review activity trend")).toBeTruthy();
  });

  it("shows the optional description", () => {
    render(ChartCardHarness, { props: { title: "Trend", description: "Submissions over time" } });
    expect(screen.getByText("Submissions over time")).toBeTruthy();
  });

  it("renders provided slot content instead of the placeholder", () => {
    render(ChartCardHarness, { props: { withContent: true } });
    expect(screen.getByTestId("chart-card-slot-content")).toBeTruthy();
    expect(screen.queryByText("No chart content provided.")).toBeNull();
  });

  it("shows a placeholder message when no content is provided", () => {
    render(ChartCardHarness, { props: { withContent: false } });
    expect(screen.getByText("No chart content provided.")).toBeTruthy();
  });
});
