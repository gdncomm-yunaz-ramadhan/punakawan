import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import DeliveryDetail from "../src/routes/deliveries/DeliveryDetail.svelte";
import { setCsrfToken } from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}


function baseView(overrides: Record<string, unknown> = {}) {
  return {
    orchestration: {
      id: "orc-1",
      revision: 1,
      status: "active",
      unresolved_inputs: [],
      created_at: "2026-08-01T00:00:00Z",
      updated_at: "2026-08-10T00:00:00Z",
    },
    title: "Migrate billing to v2",
    description: "Move every billing caller onto the v2 pricing endpoint.",
    plan_id: "delivery-plan",
    plan_revision: 3,
    projects: [{ project_id: "proj-a", attached: true, lane_ids: [], counts_by_status: {} }],
    project_plans: [],
    lanes: [],
    blockers: [],
    pending_approvals: [],
    pending_questions: [],
    next_action: "Start implementation.",
    latest_seq: 1,
    newly_runnable_lane_ids: [],
    ...overrides,
  };
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  setCsrfToken("csrf-test-token");
});
afterEach(() => {
  vi.unstubAllGlobals();
});

function mockView(view: Record<string, unknown>) {
  (fetch as unknown as Mock).mockImplementation(async (url: string) => {
    if (url === "/api/v1/deliveries/orc-1") return jsonResponse(view);
    throw new Error(`unexpected url ${url}`);
  });
}

describe("DeliveryDetail", () => {
  it("leads with recorded title and description and removes lane and approval controls", async () => {
    mockView(baseView({
      jira_activity: [{ event_type: "implementation.completed", entity_id: "task-9", issue_key: "BILL-42", fired_at: "2026-08-10T02:00:00Z" }],
      lifecycle: {
        sessions: [], usage: [{ id: "usage-1", case_id: "case-1", execution_id: "exec-1", session_id: "session-1", kind: "estimate", category: "model", quantity: 1, unit: "request", cost_amount: 12.5, cost_currency: "USD", recorded_at: "2026-08-10T02:00:00Z" }],
      },
    }));

    const { container } = render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    await waitFor(() => expect(screen.getByRole("heading", { name: "Migrate billing to v2" })).toBeTruthy());
    expect(screen.getByText("Move every billing caller onto the v2 pricing endpoint.")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "High-level plan" })).toBeTruthy();
    expect(screen.getByText("delivery-plan r3")).toBeTruthy();
    expect(screen.getByText("Estimated cost")).toBeTruthy();
    expect(container.textContent).toContain("$12.50");
    expect(screen.getByRole("table", { name: "Jira activity" })).toBeTruthy();
    expect(container.textContent).toContain("BILL-42");
    expect(container.textContent).not.toContain("Pending approvals");
    expect(container.textContent).not.toContain("Projects & lanes");
  });

  it("opens the linked-project list and sends readers to the project detail", async () => {
    mockView(baseView({
      projects: [
        { project_id: "proj-a", attached: true, lane_ids: [], counts_by_status: {} },
        { project_id: "proj-b", attached: true, lane_ids: [], counts_by_status: {} },
      ],
      project_plans: [{ project_id: "proj-a", plan_id: "plan-a", plan_revision: 2, created_at: "2026-08-10T00:00:00Z" }],
    }));
    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    await fireEvent.click(await screen.findByRole("button", { name: "View 2 projects" }));
    const dialog = screen.getByRole("dialog", { name: "Projects in this delivery" });
    const projectLink = within(dialog).getByRole("link", { name: "proj-a" });
    expect(projectLink.getAttribute("href")).toBe("/projects/proj-a");
    expect(within(dialog).getByText("proj-b")).toBeTruthy();
  });

  it("opens sessions and project plans with the required reference details", async () => {
    mockView(baseView({
      project_plans: [{ project_id: "proj-a", plan_id: "plan-a", plan_revision: 2, created_at: "2026-08-10T00:00:00Z" }],
      lifecycle: {
        sessions: [{
          id: "session-1", case_id: "case-1", execution_id: "exec-1", orchestration_id: "orc-1", participant: "codex",
          worktree_path: "/repo/billing", provider: "openai", status: "closed",
          started_at: "2026-08-10T00:00:00Z", ended_at: "2026-08-10T01:30:00Z",
        }],
        usage: [],
      },
    }));
    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    await fireEvent.click(await screen.findByRole("button", { name: "View 1 sessions" }));
    const sessions = screen.getByRole("dialog", { name: "Delivery sessions" });
    expect(within(sessions).getByRole("columnheader", { name: "Agent" })).toBeTruthy();
    expect(within(sessions).getByText("codex")).toBeTruthy();
    expect(within(sessions).getByText("/repo/billing")).toBeTruthy();
    expect(within(sessions).getByText("openai")).toBeTruthy();
    expect(within(sessions).getByText("1h 30m")).toBeTruthy();

    await fireEvent.click(await screen.findByRole("button", { name: "View 1 project plans" }));
    const plans = screen.getByRole("dialog", { name: "Project plans in this delivery" });
    const planLink = within(plans).getByRole("link", { name: "plan-a r2" });
    expect(planLink.getAttribute("href")).toBe("/projects/proj-a?tab=plans&plan=plan-a");
  });
});
