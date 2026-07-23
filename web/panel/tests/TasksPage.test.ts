import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import TasksPage from "../src/routes/tasks/TasksPage.svelte";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

const task = {
  id: "bd-1",
  title: "wire the payment webhook",
  status: "blocked",
  priority: 1,
  issue_type: "task",
  dependencies: [{ issue_id: "bd-1", depends_on_id: "bd-2", type: "blocks" }],
  created_at: "2026-07-01T00:00:00Z",
  updated_at: "2026-07-01T00:00:00Z",
  board_status: "blocked",
  blocking_reasons: ['waiting on bd-2 "prerequisite" (open)'],
  stale: false,
};

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("TasksPage", () => {
  it("renders the board with a blocked task and its reason", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockImplementation((url: string) => {
      if (url.includes("/task-graph")) return Promise.resolve(jsonResponse({ Nodes: [task], Edges: [], Cycles: [] }));
      return Promise.resolve(jsonResponse({ items: [task] }));
    });

    render(TasksPage, { props: { workspaceId: "ws-a" } });

    await waitFor(() => {
      expect(screen.getByText("wire the payment webhook")).toBeTruthy();
    });
    expect(screen.getByText('waiting on bd-2 "prerequisite" (open)')).toBeTruthy();
  });

  it("shows an error state when tasks fail to load", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ error: "boom" }, false, 500));

    render(TasksPage, { props: { workspaceId: "ws-a" } });

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toContain("boom");
    });
  });
});
