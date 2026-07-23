import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import TableCardHarness from "./fixtures/TableCardHarness.svelte";

describe("TableCard", () => {
  it("renders without crashing and shows its title", () => {
    render(TableCardHarness, { props: { title: "Current revisions" } });
    expect(screen.getByText("Current revisions")).toBeTruthy();
  });

  it("renders its content slot", () => {
    render(TableCardHarness);
    expect(screen.getByTestId("table-card-slot-content")).toBeTruthy();
  });

  it("renders the view-all action when provided", () => {
    render(TableCardHarness, { props: { withViewAll: true } });
    expect(screen.getByRole("button", { name: "View all" })).toBeTruthy();
  });
});
