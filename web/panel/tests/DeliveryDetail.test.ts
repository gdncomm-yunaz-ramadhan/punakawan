import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DeliveryDetail from "../src/routes/deliveries/DeliveryDetail.svelte";
import { setCsrfToken } from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

type FetchMock = ReturnType<typeof vi.fn>;

function orchestration(overrides: Record<string, unknown> = {}) {
  return {
    id: "orc-1",
    revision: 1,
    status: "active",
    unresolved_inputs: [],
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-10T00:00:00Z",
    ...overrides,
  };
}

function baseView(overrides: Record<string, unknown> = {}) {
  return {
    orchestration: orchestration(),
    // The view's title is always populated by the backend, derived from the
    // delivery's requirement references when nobody supplied one.
    title: "Migrate billing to v2",
    projects: [
      { project_id: "proj-a", attached: true, lane_ids: ["lane-1", "lane-2"], counts_by_status: { runnable: 1, blocked: 1 } },
    ],
    lanes: [
      {
        lane_id: "lane-1",
        project_id: "proj-a",
        status: "runnable",
        pr_url: "https://github.com/org/repo/pull/42",
        pr_number: 42,
        pr_provider: "github",
      },
      { lane_id: "lane-2", project_id: "proj-a", status: "blocked", blocked_by: ["lane-1"], parent_task_id: "task-9" },
    ],
    blockers: [{ lane_id: "lane-2", parent_task_id: "task-9", blocked_by: ["lane-1"] }],
    pending_approvals: [],
    pending_questions: [],
    next_action: "Approve proj-a to continue.",
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

describe("DeliveryDetail", () => {
  it("renders projects, lanes, blockers, PR links, and the next action", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url === "/api/v1/deliveries/orc-1") return jsonResponse(baseView());
      throw new Error(`unexpected url ${url}`);
    });

    const { container } = render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    await waitFor(() => expect(screen.getByText("Approve proj-a to continue.")).toBeTruthy());
    expect(screen.getByText("lane-1")).toBeTruthy();
    expect(screen.getAllByText("lane-2").length).toBeGreaterThan(0);
    expect(container.textContent).toContain("blocked by lane-1");
    expect(container.textContent).toContain("task task-9");

    const prLink = screen.getByRole("link", { name: /PR/ });
    expect(prLink.getAttribute("href")).toBe("https://github.com/org/repo/pull/42");
  });

  it("renders the delivery audit record in one view", async () => {
    const view = baseView({
      plan_id: "plan-billing",
      plan_revision: 3,
      lanes: [
        {
          lane_id: "lane-1",
          project_id: "proj-a",
          status: "accepted",
          repository: "org/billing",
          branch: "codex/billing-v2",
          worker: "worker-42",
          commits: ["abcdef1234567890"],
          pr_url: "https://github.com/org/billing/pull/42",
          pr_number: 42,
          verification: {
            computed_at: "2026-08-10T00:00:00Z",
            dimensions: [{ name: "unit", status: "passed" }],
          },
          bagong_review: {
            outcome: "approved",
            independence_level: "different_worker",
            reviewer_worker_id: "reviewer-7",
            reviewer_session_id: "review-session",
            blocking_finding_ids: [],
            evidence_ids: [],
            recorded_at: "2026-08-10T01:00:00Z",
          },
        },
      ],
      blockers: [],
      jira_activity: [
        {
          event_type: "implementation.completed",
          entity_id: "lane-1",
          issue_key: "BILL-42",
          fired_at: "2026-08-10T02:00:00Z",
        },
      ],
      timeline: [
        { sequence: 7, type: "lane.commit_recorded", entity_id: "lane-1", occurred_at: "2026-08-10T00:30:00Z" },
      ],
    });
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url === "/api/v1/deliveries/orc-1") return jsonResponse(view);
      throw new Error(`unexpected url ${url}`);
    });

    const { container } = render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    await waitFor(() => expect(screen.getByText("plan-billing r3")).toBeTruthy());
    expect(container.textContent).toContain("repository org/billing");
    expect(container.textContent).toContain("branch codex/billing-v2");
    expect(container.textContent).toContain("worker worker-42");
    expect(container.textContent).toContain("abcdef12");
    expect(container.textContent).toContain("unit: passed");
    expect(container.textContent).toContain("approved · different_worker");
    expect(container.textContent).toContain("BILL-42");
    expect(container.textContent).toContain("implementation.completed · lane-1");
    expect(container.textContent).toContain("lane.commit_recorded");
  });

  it("answers a pending question with the entered reference and resolution fields", async () => {
    const view = baseView({ pending_questions: ["some-ambiguous-reference"] });
    const posted: { url: string; body: Record<string, unknown> }[] = [];
    (fetch as unknown as FetchMock).mockImplementation(async (url: string, init?: RequestInit) => {
      if (url === "/api/v1/deliveries/orc-1" && (!init || !init.method || init.method === "GET")) {
        return jsonResponse(view);
      }
      if (url === "/api/v1/deliveries/orc-1/answer-question") {
        const body = JSON.parse(init!.body as string);
        posted.push({ url, body });
        return jsonResponse({ ...view, pending_questions: [] });
      }
      throw new Error(`unexpected ${url}`);
    });

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    const form = await screen.findByRole("form", { name: "Answer: some-ambiguous-reference" });
    const externalIdInput = within(form).getByPlaceholderText("PROJ-123");
    await fireEvent.input(externalIdInput, { target: { value: "PROJ-9" } });
    await fireEvent.click(within(form).getByRole("button", { name: "Answer" }));

    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0].body).toMatchObject({
      reference: "some-ambiguous-reference",
      expected_revision: 1,
      provider: "jira",
      external_id: "PROJ-9",
    });
  });

  it("approves one project's delivery independently of another project's pending approval", async () => {
    const manifestA = {
      id: "manifest-a",
      orchestration_id: "orc-1",
      project_id: "proj-a",
      parent_task_ids: [],
      planned_base_ref: "main",
      checks: [],
      status: "pending",
      created_at: "2026-08-01T00:00:00Z",
      revision: 1,
    };
    const manifestB = { ...manifestA, id: "manifest-b", project_id: "proj-b" };
    const view = baseView({
      projects: [
        { project_id: "proj-a", lane_ids: ["lane-1", "lane-2"], counts_by_status: { runnable: 1, blocked: 1 } },
        { project_id: "proj-b", lane_ids: [], counts_by_status: {} },
      ],
      pending_approvals: [manifestA, manifestB],
    });
    const posted: Record<string, unknown>[] = [];
    (fetch as unknown as FetchMock).mockImplementation(async (url: string, init?: RequestInit) => {
      if (url === "/api/v1/deliveries/orc-1" && (!init || !init.method || init.method === "GET")) {
        return jsonResponse(view);
      }
      if (url === "/api/v1/deliveries/orc-1/approve") {
        const body = JSON.parse(init!.body as string);
        posted.push(body);
        return jsonResponse({
          ...view,
          pending_approvals: view.pending_approvals.map((m) =>
            m.id === body.manifest_id ? { ...m, status: body.reject ? "rejected" : "approved", approved_by: body.approved_by } : m,
          ),
        });
      }
      throw new Error(`unexpected ${url}`);
    });

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    const approvalsRegion = await screen.findByRole("region", { name: "Pending approvals" });
    await fireEvent.input(screen.getByPlaceholderText("your name"), { target: { value: "Tester" } });

    const projectACard = within(approvalsRegion).getByText("proj-a").closest("li") as HTMLElement;
    await fireEvent.click(within(projectACard).getByRole("button", { name: "Approve" }));

    await waitFor(() => expect(posted).toEqual([{ manifest_id: "manifest-a", approved_by: "Tester", reject: false }]));

    const projectBCard = within(approvalsRegion).getByText("proj-b").closest("li") as HTMLElement;
    expect(within(projectBCard).getByRole("button", { name: "Approve" }).hasAttribute("disabled")).toBe(false);
  });

  it("renders worker, worktree, base sha, pipeline stage, repair count, escalation, and evidence links", async () => {
    const view = baseView({
      lanes: [
        {
          lane_id: "lane-1",
          project_id: "proj-a",
          status: "running",
          worker: "worker-42",
          worktree_path: "/tmp/worktrees/lane-1",
          base_sha: "abcdef1234567890",
          base_remote: "origin",
          semar_record_id: "rec-semar",
          gareng_record_id: "rec-gareng",
          attempt: 2,
          repair_cycle_count: 1,
          escalated_at: "2026-08-10T00:00:00Z",
          evidence: [{ id: "ev-1", kind: "test", media_type: "application/json", byte_size: 512, content_hash: "sha256:abc" }],
        },
      ],
    });
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url === "/api/v1/deliveries/orc-1") return jsonResponse(view);
      throw new Error(`unexpected url ${url}`);
    });

    const { container } = render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    await waitFor(() => expect(screen.getByText("lane-1")).toBeTruthy());
    expect(container.textContent).toContain("worker worker-42");
    expect(container.textContent).toContain("worktree /tmp/worktrees/lane-1");
    expect(container.textContent).toContain("base abcdef12 (origin)");
    expect(container.textContent).toContain("stage: Semar → Gareng");
    expect(container.textContent).toContain("attempt 2");
    expect(container.textContent).toContain("1 repair cycle");
    expect(container.textContent).toContain("escalated at 2026-08-10T00:00:00Z");

    const evidenceLink = screen.getByRole("link", { name: /test evidence/ });
    expect(evidenceLink.getAttribute("href")).toBe("/api/v1/deliveries/orc-1/evidence/ev-1");
  });

  it("leads with the title and shows the description, session, and plan record when present", async () => {
    const view = baseView({
      description: "Move every billing caller onto the v2 pricing endpoint.",
      session_id: "pkw:run/ws-1/billing-42",
      plan_record_id: "rec-plan-7",
    });
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url === "/api/v1/deliveries/orc-1") return jsonResponse(view);
      throw new Error(`unexpected url ${url}`);
    });

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    await waitFor(() => expect(screen.getByText("Migrate billing to v2")).toBeTruthy());
    expect(screen.getByTestId("delivery-description").textContent).toBe(
      "Move every billing caller onto the v2 pricing endpoint.",
    );
    const references = screen.getByTestId("delivery-references");
    expect(references.textContent).toContain("pkw:run/ws-1/billing-42");
    expect(references.textContent).toContain("rec-plan-7");
    expect(references.textContent).not.toContain("Not recorded");
  });

  it("omits the description entirely and says so plainly when the session and plan record are absent", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url === "/api/v1/deliveries/orc-1") return jsonResponse(baseView());
      throw new Error(`unexpected url ${url}`);
    });

    const { container } = render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    await waitFor(() => expect(screen.getByTestId("delivery-references")).toBeTruthy());
    expect(screen.queryByTestId("delivery-description")).toBeNull();

    const references = screen.getByTestId("delivery-references");
    expect(within(references).getAllByText("Not recorded")).toHaveLength(2);
    expect(container.textContent).not.toContain("undefined");
  });

  it("distinguishes an attached project from one that only has lanes here", async () => {
    const view = baseView({
      projects: [
        { project_id: "proj-a", attached: true, lane_ids: [], counts_by_status: {} },
        { project_id: "proj-b", attached: false, lane_ids: ["lane-1"], counts_by_status: { accepted: 1 } },
      ],
      lanes: [{ lane_id: "lane-1", project_id: "proj-b", status: "accepted" }],
      blockers: [],
    });
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url === "/api/v1/deliveries/orc-1") return jsonResponse(view);
      throw new Error(`unexpected url ${url}`);
    });

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    await waitFor(() => expect(screen.getByTestId("attached-proj-a")).toBeTruthy());
    expect(screen.getByTestId("attached-proj-a").textContent).toContain("Attached");
    expect(screen.queryByTestId("attached-proj-b")).toBeNull();
    expect(screen.getByTestId("unattached-proj-b").textContent).toContain("Not attached");
    // An attached project with no lanes is still listed, rather than dropping
    // out of the view because nothing is running there yet.
    expect(screen.getByText("No lanes yet.")).toBeTruthy();
  });

  it("shows the session that opened a lane separately from the worker holding its lease", async () => {
    const view = baseView({
      lanes: [
        {
          lane_id: "lane-1",
          project_id: "proj-a",
          status: "running",
          session_id: "pkw:run/ws-1/billing-42",
          worker: "worker-42",
        },
      ],
      blockers: [],
    });
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url === "/api/v1/deliveries/orc-1") return jsonResponse(view);
      throw new Error(`unexpected url ${url}`);
    });

    const { container } = render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    await waitFor(() => expect(screen.getByText("lane-1")).toBeTruthy());
    expect(container.textContent).toContain("session pkw:run/ws-1/billing-42");
    expect(container.textContent).toContain("worker worker-42");
  });

  it("leaves a lane's session out when it was opened without one", async () => {
    const view = baseView({
      lanes: [{ lane_id: "lane-1", project_id: "proj-a", status: "running", worker: "worker-42" }],
      blockers: [],
    });
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url === "/api/v1/deliveries/orc-1") return jsonResponse(view);
      throw new Error(`unexpected url ${url}`);
    });

    const { container } = render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    await waitFor(() => expect(screen.getByText("lane-1")).toBeTruthy());
    expect(container.textContent).toContain("worker worker-42");
    expect(container.textContent).not.toContain("session ");
  });
});
