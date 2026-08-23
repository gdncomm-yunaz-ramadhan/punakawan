import { fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectTasks from "../src/routes/projects/ProjectTasks.svelte";

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

type FetchMock = ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ProjectTasks", () => {
  it("renders the board with a blocked task and its reason", async () => {
    (fetch as unknown as FetchMock).mockImplementation((url: string) => {
      if (url.includes("/task-graph")) return Promise.resolve(jsonResponse({ nodes: [task], edges: [], cycles: [] }));
      return Promise.resolve(jsonResponse({ items: [task] }));
    });

    render(ProjectTasks, { props: { projectId: "proj-a" } });

    await waitFor(() => {
      expect(screen.getByText("wire the payment webhook")).toBeTruthy();
    });

    await fireEvent.click(screen.getByRole("tab", { name: "Board" }));

    await waitFor(() => {
      expect(screen.getByText('waiting on bd-2 "prerequisite" (open)')).toBeTruthy();
    });
  });

  it("re-fetches with the selected status and priority filters", async () => {
    (fetch as unknown as FetchMock).mockImplementation((url: string) => {
      if (url.includes("/task-graph")) return Promise.resolve(jsonResponse({ nodes: [task], edges: [], cycles: [] }));
      return Promise.resolve(jsonResponse({ items: [task] }));
    });

    render(ProjectTasks, { props: { projectId: "proj-a" } });
    await waitFor(() => expect(screen.getByText("wire the payment webhook")).toBeTruthy());

    const statusSelect = screen.getByDisplayValue("Any status");
    await fireEvent.change(statusSelect, { target: { value: "blocked" } });

    await waitFor(() => {
      const call = (fetch as unknown as FetchMock).mock.calls.find((c) => String(c[0]).includes("/projects/proj-a/tasks?"));
      expect(call).toBeTruthy();
      expect(String(call![0])).toContain("status=blocked");
    });
  });

  it("shows an error state when tasks fail to load", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse({ error: "boom" }, false, 500));

    render(ProjectTasks, { props: { projectId: "proj-a" } });

    await waitFor(() => {
      expect(screen.getByText(/Failed to load tasks/)).toBeTruthy();
      expect(screen.getByText(/boom/)).toBeTruthy();
    });
  });
});
