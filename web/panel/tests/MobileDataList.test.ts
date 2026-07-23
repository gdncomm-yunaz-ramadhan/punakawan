import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import MobileDataListHarness from "./fixtures/MobileDataListHarness.svelte";

const rows = [
  { id: "task-1", title: "Add checkout API contract", status: "in_progress", priority: "P1", owner: "alice", updated: "2026-07-20" },
  { id: "task-2", title: "Fix flaky test", status: "blocked", priority: "P2", owner: "bob", updated: "2026-07-21" },
];

describe("MobileDataList", () => {
  it("renders without crashing as a list of cards, not a table", () => {
    render(MobileDataListHarness, { props: { rows } });
    expect(screen.getByTestId("mobile-data-list")).toBeTruthy();
    expect(document.querySelector("table")).toBeNull();
  });

  it("shows the primary field as each card's title", () => {
    render(MobileDataListHarness, { props: { rows } });
    expect(screen.getByText("Add checkout API contract")).toBeTruthy();
    expect(screen.getByText("Fix flaky test")).toBeTruthy();
  });

  it("shows up to three secondary fields inline per card", () => {
    render(MobileDataListHarness, { props: { rows } });
    expect(screen.getByText("in_progress")).toBeTruthy();
    expect(screen.getByText("P1")).toBeTruthy();
    expect(screen.getByText("alice")).toBeTruthy();
  });

  it("hides overflow fields behind an expandable 'Show more' toggle", async () => {
    render(MobileDataListHarness, { props: { rows } });
    expect(screen.queryByText("2026-07-20")).toBeNull();
    const toggles = screen.getAllByRole("button", { name: "Show more" });
    await fireEvent.click(toggles[0]);
    expect(screen.getByText("2026-07-20")).toBeTruthy();
  });

  it("renders a visible primary action per row when provided", async () => {
    render(MobileDataListHarness, { props: { rows, withAction: true } });
    const actions = screen.getAllByRole("button", { name: "Open" });
    expect(actions).toHaveLength(2);
    await fireEvent.click(actions[0]);
    expect(screen.getByTestId("last-selected").textContent).toBe("task-1");
  });

  it("shows an empty message when there are no rows", () => {
    render(MobileDataListHarness, { props: { rows: [] } });
    expect(screen.getByTestId("mobile-list-empty")).toBeTruthy();
  });
});
