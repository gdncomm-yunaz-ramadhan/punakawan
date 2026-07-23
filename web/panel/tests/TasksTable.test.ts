import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it, vi } from "vitest";
import TasksTable from "../src/routes/tasks/TasksTable.svelte";
import type { TaskSummary } from "../src/lib/api/client";

function makeTask(overrides: Partial<TaskSummary> = {}): TaskSummary {
  return {
    id: "bd-1",
    title: "wire the payment webhook",
    status: "open",
    priority: 1,
    issue_type: "task",
    dependencies: [{ issue_id: "bd-1", depends_on_id: "bd-2", type: "blocks" }],
    created_at: "2026-07-01T00:00:00Z",
    updated_at: "2026-07-01T00:00:00Z",
    board_status: "blocked",
    stale: false,
    ...overrides,
  };
}

describe("TasksTable", () => {
  it("renders without crashing, using the shared DataTable end to end against real task data", () => {
    render(TasksTable, { props: { tasks: [makeTask()], onselect: vi.fn() } });
    expect(screen.getByText("wire the payment webhook")).toBeTruthy();
    expect(screen.getByText("blocked")).toBeTruthy();
    expect(screen.getByText("P1")).toBeTruthy();
  });

  it("shows the empty message when there are no tasks", () => {
    render(TasksTable, { props: { tasks: [], onselect: vi.fn() } });
    expect(screen.getByText("No tasks match these filters.")).toBeTruthy();
  });

  it("calls onselect with the task id when the row action is activated", async () => {
    const onselect = vi.fn();
    render(TasksTable, { props: { tasks: [makeTask({ id: "bd-7" })], onselect } });
    await fireEvent.click(screen.getByRole("button", { name: "Open" }));
    expect(onselect).toHaveBeenCalledWith("bd-7");
  });

  it("sorts by title when the Title header is clicked", async () => {
    const tasks = [makeTask({ id: "bd-1", title: "Zebra task" }), makeTask({ id: "bd-2", title: "Alpha task" })];
    render(TasksTable, { props: { tasks, onselect: vi.fn() } });

    await fireEvent.click(screen.getByRole("button", { name: /Title/ }));
    const firstRowCells = document.querySelectorAll("tbody tr:first-child td");
    expect(firstRowCells[1].textContent).toBe("Alpha task");
  });
});
