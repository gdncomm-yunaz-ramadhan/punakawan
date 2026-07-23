import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SessionDetail from "../src/routes/sessions/SessionDetail.svelte";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("SessionDetail", () => {
  it("renders progress, timeline, role lanes, capsules and errors", async () => {
    const session = {
      id: "run-1",
      workspace_id: "ws-a",
      workflow: "feature-delivery",
      status: "executing",
      started_at: "2026-07-23T00:00:00Z",
      updated_at: "2026-07-23T00:05:00Z",
      objective: "ship the refund flow",
      active_role: "petruk",
      task_counts: { total: 4, open: 1, in_progress: 1, blocked: 0, closed: 2 },
      evidence_count: 2,
      error_count: 1,
      Timeline: [
        {
          id: "evt-1",
          run_id: "run-1",
          timestamp: "2026-07-23T00:01:00Z",
          operation: "plan_submitted",
          type: "plan",
          result: "success",
          role: "petruk",
          task: "bd-task-1",
        },
        {
          id: "evt-2",
          run_id: "run-1",
          timestamp: "2026-07-23T00:02:00Z",
          operation: "tests_run",
          type: "review",
          result: "failure",
          role: "gareng",
          task: "bd-task-1",
        },
      ],
    };
    const capsules = {
      items: [
        {
          id: "cap-1",
          task_id: "bd-task-1",
          created_at: "2026-07-23T00:00:30Z",
          digest: "sha256:" + "a".repeat(64),
          role: "petruk",
          objective: "implement the refund flow",
          allowed_tools: ["run_tests"],
          forbidden_actions: [],
          relevant_knowledge: [{ id: "kn-1" }],
          evidence: [],
        },
      ],
    };

    (fetch as unknown as ReturnType<typeof vi.fn>).mockImplementation((url: string) => {
      if (url.includes("/capsules")) return Promise.resolve(jsonResponse(capsules));
      return Promise.resolve(jsonResponse(session));
    });

    render(SessionDetail, { props: { workspaceId: "ws-a", sessionId: "run-1" } });

    await waitFor(() => {
      expect(screen.getByText("run-1")).toBeTruthy();
    });
    expect(screen.getByText("ship the refund flow")).toBeTruthy();
    expect(screen.getAllByText("plan_submitted").length).toBeGreaterThan(0);
    expect(screen.getAllByText("tests_run").length).toBeGreaterThan(0);
    expect(screen.getByText("implement the refund flow")).toBeTruthy();
    expect(screen.getByText(/1 total error/)).toBeTruthy();
  });

  it("shows an error state when the session call fails", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ error: "not found" }, false, 404));

    render(SessionDetail, { props: { workspaceId: "ws-a", sessionId: "no-such-run" } });

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toContain("not found");
    });
  });
});
