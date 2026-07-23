import { fireEvent, render, screen, within } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import DataTableHarness from "./fixtures/DataTableHarness.svelte";

function makeRows(count: number) {
  return Array.from({ length: count }, (_, i) => ({
    id: `task-${i + 1}`,
    title: `Task ${i + 1}`,
    status: i % 2 === 0 ? "open" : "blocked",
    priority: i % 5,
    updated: `2026-07-${String((i % 28) + 1).padStart(2, "0")}`,
  }));
}

describe("DataTable", () => {
  it("renders without crashing as a table on a wide viewport", () => {
    render(DataTableHarness, { props: { rows: makeRows(3), forceWidth: 1024 } });
    expect(document.querySelector("table")).toBeTruthy();
    expect(screen.getByText("Task 1")).toBeTruthy();
  });

  it("sorts rows ascending then descending when a sortable header is clicked", async () => {
    render(DataTableHarness, { props: { rows: makeRows(3), forceWidth: 1024 } });
    const header = screen.getByRole("button", { name: /Title/ });

    await fireEvent.click(header);
    let cells = document.querySelectorAll("tbody tr td:first-child");
    expect(cells[0].textContent).toBe("Task 1");
    expect(cells[2].textContent).toBe("Task 3");

    await fireEvent.click(header);
    cells = document.querySelectorAll("tbody tr td:first-child");
    expect(cells[0].textContent).toBe("Task 3");
    expect(cells[2].textContent).toBe("Task 1");
  });

  it("does not sort a non-sortable column", async () => {
    render(DataTableHarness, { props: { rows: makeRows(3), forceWidth: 1024 } });
    // "Updated" has no sortable:true, so it renders as plain text, not a button.
    expect(screen.queryByRole("button", { name: /Updated/ })).toBeNull();
    expect(screen.getByText("Updated")).toBeTruthy();
  });

  it("paginates: shows only pageSize rows and advances on Next", async () => {
    render(DataTableHarness, { props: { rows: makeRows(15), pageSize: 10, forceWidth: 1024 } });
    expect(document.querySelectorAll("tbody tr")).toHaveLength(10);

    await fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(screen.getByTestId("current-page").textContent).toBe("2");
    expect(document.querySelectorAll("tbody tr")).toHaveLength(5);
  });

  it("renders MobileDataList instead of a <table> under the mobile breakpoint", () => {
    render(DataTableHarness, { props: { rows: makeRows(3), forceWidth: 375 } });
    expect(document.querySelector("table")).toBeNull();
    expect(screen.getByTestId("mobile-data-list")).toBeTruthy();
  });

  it("renders a <table> at/above the mobile breakpoint", () => {
    render(DataTableHarness, { props: { rows: makeRows(3), forceWidth: 640 } });
    expect(document.querySelector("table")).toBeTruthy();
    expect(screen.queryByTestId("mobile-data-list")).toBeNull();
  });

  it("shows a loading skeleton and no table when loading", () => {
    render(DataTableHarness, { props: { rows: [], loading: true, forceWidth: 1024 } });
    expect(screen.getByTestId("data-table-skeleton")).toBeTruthy();
    expect(document.querySelector("table")).toBeNull();
  });

  it("shows an error message when error is set", () => {
    render(DataTableHarness, { props: { rows: [], error: "Failed to load", forceWidth: 1024 } });
    expect(screen.getByRole("alert").textContent).toContain("Failed to load");
  });

  it("shows an empty message when there are no rows", () => {
    render(DataTableHarness, { props: { rows: [], forceWidth: 1024 } });
    expect(screen.getByTestId("data-table-empty")).toBeTruthy();
  });

  it("adds a selection checkbox column when selectable, and reports selection changes", async () => {
    render(DataTableHarness, { props: { rows: makeRows(2), selectable: true, forceWidth: 1024 } });
    const checkboxes = screen.getAllByRole("checkbox", { name: "Select row" });
    expect(checkboxes).toHaveLength(2);

    await fireEvent.click(checkboxes[0]);
    expect(screen.getByTestId("selection-count").textContent).toBe("1");
  });

  it("exposes row actions as keyboard-reachable buttons, not just click/hover", async () => {
    render(DataTableHarness, { props: { rows: makeRows(2), withAction: true, forceWidth: 1024 } });
    const actions = screen.getAllByRole("button", { name: "Open" });
    expect(actions).toHaveLength(2);
    // A real <button> is Tab-reachable by default; confirm it's not disabled
    // and responds to activation the same way keyboard Enter/Space would.
    expect(actions[0].tagName).toBe("BUTTON");
    await fireEvent.click(actions[0]);
    expect(screen.getByTestId("last-selected").textContent).toBe("task-1");
  });

  it("applies compact density padding via a data attribute", () => {
    render(DataTableHarness, { props: { rows: makeRows(2), density: "compact", forceWidth: 1024 } });
    expect(document.querySelector(".data-table")?.getAttribute("data-density")).toBe("compact");
  });

  it("toggles column visibility via the Columns menu", async () => {
    render(DataTableHarness, { props: { rows: makeRows(2), forceWidth: 1024 } });
    await fireEvent.click(screen.getByRole("button", { name: "Columns" }));
    const statusCheckbox = screen.getByRole("checkbox", { name: "Status" });
    expect(statusCheckbox).toBeTruthy();

    // Status column visible initially.
    expect(screen.getAllByText("open").length).toBeGreaterThan(0);
    await fireEvent.click(statusCheckbox);
    expect(screen.queryByText("open")).toBeNull();
  });
});
