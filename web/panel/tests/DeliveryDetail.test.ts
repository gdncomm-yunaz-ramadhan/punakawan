import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import DeliveryDetail from "../src/routes/deliveries/DeliveryDetail.svelte";
import { setCsrfToken } from "../src/lib/session";

function response(body: unknown) { return { ok: true, status: 200, json: async () => body } as Response; }

function view(overrides: Record<string, unknown> = {}) {
  return {
    orchestration: { id: "orc-1", revision: 1, status: "active", unresolved_inputs: [], created_at: "2026-08-01T00:00:00Z", updated_at: "2026-08-10T00:00:00Z" },
    title: "Migrate billing to v2", description: "Move every billing caller onto the v2 pricing endpoint.", plan_id: "delivery-plan", plan_revision: 3,
    projects: [{ project_id: "billing", attached: true, lane_ids: [], counts_by_status: {} }],
    project_plans: [{ project_id: "billing", plan_id: "plan-billing", plan_revision: 2, created_at: "2026-08-10T00:00:00Z" }],
    lanes: [], blockers: [], pending_questions: [], next_action: "", latest_seq: 1, newly_runnable_lane_ids: [],
    jira_activity: [{ issue_key: "BILL-42", event_type: "implementation.completed", entity_id: "task-9", fired_at: "2026-08-10T02:00:00Z" }],
    lifecycle: { sessions: [{ id: "session-1", case_id: "case-1", execution_id: "exec-1", orchestration_id: "orc-1", participant: "codex", worktree_path: "/repo/billing", provider: "openai", status: "closed", started_at: "2026-08-10T00:00:00Z", ended_at: "2026-08-10T01:30:00Z" }], usage: [{ id: "usage-1", case_id: "case-1", execution_id: "exec-1", session_id: "session-1", kind: "estimate", category: "model", quantity: 1, unit: "request", cost_amount: 12.5, cost_currency: "USD", recorded_at: "2026-08-10T02:00:00Z" }] },
    ...overrides,
  };
}

beforeEach(() => { vi.stubGlobal("fetch", vi.fn()); setCsrfToken("csrf-test-token"); });
afterEach(() => vi.unstubAllGlobals());

function renderView(data = view()) {
  (fetch as unknown as Mock).mockImplementation(async (url: string) => {
    if (url === "/api/v1/deliveries/orc-1") return response(data);
    throw new Error(`unexpected url ${url}`);
  });
  return render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });
}

describe("DeliveryDetail", () => {
  it("renders a responsive bento summary with title, description, plan, and estimates", async () => {
    const { container } = renderView();
    await waitFor(() => expect(screen.getByRole("heading", { name: "Migrate billing to v2" })).toBeTruthy());
    expect(screen.getByText("Move every billing caller onto the v2 pricing endpoint.")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "High-level plan" })).toBeTruthy();
    expect(screen.getByText("delivery-plan r3")).toBeTruthy();
    expect(container.querySelectorAll(".bento-card")).toHaveLength(4);
    expect(container.textContent).toContain("$12.50");
    expect(screen.queryByText("Pending approvals")).toBeNull();
    expect(screen.queryByText("Projects & lanes")).toBeNull();
  });

  it("renders each delivery record in its matching tab", async () => {
    renderView();
    await screen.findByRole("tab", { name: "Projects" });

    await fireEvent.click(screen.getByRole("tab", { name: "Projects" }));
    const projects = screen.getByRole("tabpanel", { name: "Projects" });
    expect(within(projects).getByRole("link", { name: "billing" }).getAttribute("href")).toBe("/projects/billing");

    await fireEvent.click(screen.getByRole("tab", { name: "Plans" }));
    const plans = screen.getByRole("tabpanel", { name: "Plans" });
    expect(within(plans).getByRole("link", { name: "plan-billing r2" }).getAttribute("href")).toBe("/projects/billing?tab=plans&plan=plan-billing");

    await fireEvent.click(screen.getByRole("tab", { name: "Sessions" }));
    const sessions = screen.getByRole("tabpanel", { name: "Sessions" });
    expect(within(sessions).getByText("codex")).toBeTruthy();
    expect(within(sessions).getByText("/repo/billing")).toBeTruthy();
    expect(within(sessions).getByText("openai")).toBeTruthy();

    await fireEvent.click(screen.getByRole("tab", { name: "Activities" }));
    const activities = screen.getByRole("tabpanel", { name: "Activities" });
    expect(within(activities).getByRole("table", { name: "Jira activity" })).toBeTruthy();
    expect(within(activities).getByText("BILL-42")).toBeTruthy();
  });
});
