import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DeliveryDetail from "../src/routes/deliveries/DeliveryDetail.svelte";
import { setCsrfToken } from "../src/lib/session";

// Real EventSource frames always carry an explicit `event: <type>` line
// (internal/panel/events/sse.go), so jsdom's absence of EventSource
// entirely (confirmed: no test in this suite mocks it) is not the
// blocker for exercising AC4's live-unlock path - the module boundary
// is. Mocking sse.svelte's tiny public surface lets this test dispatch
// a synthetic delivery.updated frame straight at whatever listener
// DeliveryDetail registered via onPanelEvent, the same way the real
// module would after a live SSE frame arrives.
let panelListeners: Array<(evt: MessageEvent) => void> = [];

vi.mock("../src/lib/events/sse.svelte", () => ({
  onPanelEvent: (cb: (evt: MessageEvent) => void) => {
    panelListeners.push(cb);
    return () => {
      panelListeners = panelListeners.filter((l) => l !== cb);
    };
  },
  parsePanelEvent: (evt: MessageEvent) => {
    try {
      return JSON.parse((evt as unknown as { data: string }).data);
    } catch {
      return null;
    }
  },
}));

function emitPanelEvent(type: string, data: unknown) {
  const evt = { type, data: JSON.stringify(data) } as unknown as MessageEvent;
  for (const cb of [...panelListeners]) cb(evt);
}

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
    projects: [{ project_id: "proj-a", lane_ids: ["lane-1", "lane-2"], counts_by_status: { runnable: 1, blocked: 1 } }],
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
  panelListeners = [];
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

  it("live-unlocks a newly runnable lane on a matching delivery.updated SSE event, without any manual refresh", async () => {
    const blockedView = baseView({
      lanes: [{ lane_id: "lane-2", project_id: "proj-a", status: "blocked", blocked_by: ["lane-1"] }],
      latest_seq: 1,
    });
    const unlockedView = baseView({
      lanes: [{ lane_id: "lane-2", project_id: "proj-a", status: "runnable" }],
      latest_seq: 2,
      newly_runnable_lane_ids: ["lane-2"],
    });

    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url === "/api/v1/deliveries/orc-1") return jsonResponse(blockedView);
      if (url === "/api/v1/deliveries/orc-1?since_seq=1") return jsonResponse(unlockedView);
      throw new Error(`unexpected url ${url}`);
    });

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    await waitFor(() => expect(screen.getByText("blocked")).toBeTruthy());
    expect(panelListeners.length).toBeGreaterThan(0);

    emitPanelEvent("delivery.updated", {
      id: "evt-2",
      type: "delivery.updated",
      entity_id: "orc-1",
      payload: { latest_seq: 2, newly_runnable_lane_ids: ["lane-2"] },
    });

    await waitFor(() => expect(screen.getByText("runnable")).toBeTruthy());
    expect(screen.getByText("Newly runnable")).toBeTruthy();
  });
});
