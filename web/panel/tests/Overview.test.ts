import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Overview from "../src/routes/overview/Overview.svelte";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Overview", () => {
  it("orders Needs Attention and shows summary counts", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({
        active_sessions: [{ id: "run-1", workspace_id: "ws-a", workflow: "feature-delivery", status: "executing", started_at: "2026-07-23T00:00:00Z", updated_at: "2026-07-23T00:00:00Z" }],
        pending_approvals: [],
        blocked_tasks: 3,
        available_workspaces: 1,
        needs_attention: [
          { kind: "failed_session", workspace_id: "ws-a", entity_id: "run-2", message: "session failed" },
          { kind: "blocked_tasks", workspace_id: "ws-a", message: "3 blocked tasks" },
        ],
        workspace_health: [{ id: "ws-a", path: "/repos/ws-a", display_name: "WS A", availability: "available", repository_count: 1, active_session_count: 1, open_task_count: 2, blocked_task_count: 3, knowledge_count: 5, last_activity_at: "2026-07-23T00:00:00Z", pinned: false }],
        recent_sessions: [],
      }),
    );

    render(Overview);

    await waitFor(() => {
      expect(screen.getByText("3")).toBeTruthy();
    });
    expect(screen.getByText("session failed")).toBeTruthy();
    expect(screen.getByText("3 blocked tasks")).toBeTruthy();
    expect(screen.getByText("WS A")).toBeTruthy();
  });

  it("shows empty states when nothing is active or needs attention", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({
        active_sessions: [],
        pending_approvals: [],
        blocked_tasks: 0,
        available_workspaces: 0,
        needs_attention: [],
        workspace_health: [],
        recent_sessions: [],
      }),
    );

    render(Overview);

    await waitFor(() => {
      expect(screen.getByText("No active sessions.")).toBeTruthy();
    });
    expect(screen.getByText("Nothing needs attention.")).toBeTruthy();
    expect(screen.getByText("No sessions yet.")).toBeTruthy();
  });

  it("shows an error state when the overview call fails", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ error: "boom" }, false, 500));

    render(Overview);

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toContain("boom");
    });
  });
});
