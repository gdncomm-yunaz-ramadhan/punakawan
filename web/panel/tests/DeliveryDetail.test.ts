import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import DeliveryDetail from "../src/routes/deliveries/DeliveryDetail.svelte";
import { setCsrfToken } from "../src/lib/session";
import type { DeliveryDetail as DeliveryDetailModel } from "../src/lib/api/client";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

function detail(over: Partial<DeliveryDetailModel> = {}): DeliveryDetailModel {
  return {
    id: "orc-1",
    title: "Migrate billing to v2",
    status: "active",
    projects: [],
    usage: {
      input_tokens: 800,
      output_tokens: 200,
      cache_tokens: 0,
      tool_calls: 5,
      elapsed_ms: 90_000,
      estimated_costs: { USD: 12.5 },
      pricing_complete: true,
    },
    updated_at: "2026-08-10T00:00:00Z",
    cancellable: true,
    projection_revision: 1,
    orchestration_revision: 1,
    description: "Move every billing caller onto the v2 pricing endpoint.",
    activity: [],
    ...over,
  } as DeliveryDetailModel;
}

type FetchMock = ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  setCsrfToken("csrf-test-token");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function installBackend(d: DeliveryDetailModel, opts: { onPost?: (url: string) => Response } = {}) {
  (fetch as unknown as FetchMock).mockImplementation(async (url: string, init?: RequestInit) => {
    const method = (init?.method ?? "GET").toUpperCase();
    if (method === "POST") {
      if (opts.onPost) return opts.onPost(url);
      return jsonResponse(d);
    }
    if (url.startsWith("/api/v1/deliveries/orc-1")) return jsonResponse(d);
    throw new Error(`unexpected url ${url}`);
  });
}

describe("DeliveryDetail", () => {
  it("renders required tabs and their empty states when no delivery data exists", async () => {
    installBackend(detail());

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    await waitFor(() => expect(screen.getByRole("heading", { name: "Migrate billing to v2" })).toBeTruthy());
    expect(screen.getByText("Move every billing caller onto the v2 pricing endpoint.")).toBeTruthy();
    for (const tab of ["Overview", "Projects", "Jira", "Sessions", "Activity"]) {
      expect(screen.getByRole("tab", { name: tab })).toBeTruthy();
    }
    expect(screen.queryByRole("tab", { name: "Plan" })).toBeNull();
    expect(screen.queryByRole("tab", { name: "GitHub" })).toBeNull();
    expect(screen.getByText("Estimated cost")).toBeTruthy();
    expect(screen.queryByText("Source")).toBeNull();
    expect(screen.queryByText("Tool calls")).toBeNull();
    expect(screen.queryByText("Tokens")).toBeNull();
    expect(screen.queryByText("Elapsed")).toBeNull();

    await fireEvent.click(screen.getByRole("tab", { name: "Projects" }));
    expect(screen.getByText("No projects linked to this delivery.")).toBeTruthy();
    await fireEvent.click(screen.getByRole("tab", { name: "Jira" }));
    expect(screen.getByText("No Jira activity recorded for this delivery.")).toBeTruthy();
    await fireEvent.click(screen.getByRole("tab", { name: "Sessions" }));
    expect(screen.getByText("No sessions recorded.")).toBeTruthy();
    await fireEvent.click(screen.getByRole("tab", { name: "Activity" }));
    expect(screen.getByText("No activity recorded.")).toBeTruthy();
  });

  it("opens cost detail from the estimated cost info button", async () => {
    installBackend(detail());

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });
    await screen.findByRole("heading", { name: "Migrate billing to v2" });

    await fireEvent.click(screen.getByRole("tab", { name: "Overview" }));
    await fireEvent.click(screen.getByLabelText("Cost detail"));
    const dialog = screen.getByRole("dialog", { name: "Estimated cost detail" });
    expect(dialog.textContent).toContain("Tokens");
    expect(dialog.textContent).toContain("$12.50");
    expect(dialog.textContent).toContain("Elapsed time");
    expect(dialog.textContent).toContain("Tool calls");
  });

  it("shows total linked projects, plans, and sessions in overview metrics", async () => {
    installBackend(
      detail({
        projects: [{ id: "billing", slug: "billing" }, { id: "checkout", slug: "checkout" }],
        project_plans: [
          { project_id: "billing", project_slug: "billing", plan: { id: "plan-billing", objective: "Ship billing", revision: 1 }, head_revision: 1 },
          { project_id: "checkout", project_slug: "checkout", plan: { id: "plan-checkout", objective: "Ship checkout", revision: 1 }, head_revision: 1 },
        ],
        sessions: [
          { id: "session-1", case_id: "case-1", execution_id: "exec-1", orchestration_id: "orc-1", participant: "codex", status: "closed", started_at: "2026-08-10T00:00:00Z", checkpoints: [] },
          { id: "session-2", case_id: "case-1", execution_id: "exec-1", orchestration_id: "orc-1", participant: "codex", status: "closed", started_at: "2026-08-10T01:00:00Z", checkpoints: [] },
          { id: "session-3", case_id: "case-1", execution_id: "exec-1", orchestration_id: "orc-1", participant: "codex", status: "closed", started_at: "2026-08-10T02:00:00Z", checkpoints: [] },
        ],
      }),
    );

    const { container } = render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });
    await screen.findByRole("heading", { name: "Migrate billing to v2" });
    await fireEvent.click(screen.getByRole("tab", { name: "Overview" }));

    const metricValue = (label: string) => {
      for (const metric of container.querySelectorAll(".metric")) {
        if (metric.querySelector(".label")?.textContent === label) return metric.querySelector(".value")?.textContent;
      }
      return undefined;
    };
    expect(metricValue("Total projects")).toBe("2");
    expect(metricValue("Total plans")).toBe("2");
    expect(metricValue("Total sessions")).toBe("3");
  });

  it("never renders lane/blocked/pending-question language anywhere on the page", async () => {
    installBackend(detail());

    const { container } = render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });
    await waitFor(() => expect(screen.getByRole("heading", { name: "Migrate billing to v2" })).toBeTruthy());

    const text = container.textContent?.toLowerCase() ?? "";
    expect(text).not.toContain("lane");
    expect(text).not.toContain("blocked");
    expect(text).not.toContain("pending question");
  });

  it("shows a Projects tab with each project's linked plan", async () => {
    installBackend(
      detail({
        projects: [{ id: "billing", slug: "billing" }],
        project_plans: [
          {
            project_id: "billing",
            project_slug: "billing",
            plan: { id: "plan-billing", objective: "Ship billing v2", revision: 2 },
            head_revision: 2,
          },
        ],
      }),
    );

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });
    await screen.findByRole("tab", { name: "Projects" });

    await fireEvent.click(screen.getByRole("tab", { name: "Projects" }));
    const panel = screen.getByRole("tabpanel", { name: "Projects" });
    expect(within(panel).getByText("billing")).toBeTruthy();
    expect(within(panel).getByText("Ship billing v2", { exact: false })).toBeTruthy();

    await fireEvent.click(within(panel).getByRole("button", { name: "Open" }));
    expect(window.location.pathname).toBe("/projects/billing");
  });

  it("shows a Jira tab with touched items and transitions", async () => {
    installBackend(
      detail({
        jira: {
          issue_key: "BILL-42",
          touched_items: [{ parent_task_id: "PUN-12", jira_issue_key: "BILL-42", touch_count: 3 }],
          transitions: [{ to_status: "In Progress", status: "succeeded", occurred_at: "2026-08-10T02:00:00Z" }],
          worklogs: [],
          write_health: { pending: 0, retrying: 0, reconciling: 0, failed: 0, succeeded: 1, cancelled: 0 },
        },
      }),
    );

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });
    await screen.findByRole("tab", { name: "Jira" });

    await fireEvent.click(screen.getByRole("tab", { name: "Jira" }));
    const panel = screen.getByRole("tabpanel", { name: "Jira" });
    expect(within(panel).getAllByText("BILL-42", { exact: false }).length).toBeGreaterThan(0);
    expect(within(panel).getByText("PUN-12")).toBeTruthy();
    expect(within(panel).getByText("In Progress")).toBeTruthy();
  });

  it("renders a Jira empty state when touched_items is null", async () => {
    installBackend(
      detail({
        jira: {
          issue_key: "BILL-42",
          touched_items: null as unknown as [],
          transitions: [],
          worklogs: [],
          write_health: { pending: 0, retrying: 0, reconciling: 0, failed: 0, succeeded: 0, cancelled: 0 },
        },
      }),
    );

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });
    await screen.findByRole("tab", { name: "Jira" });
    await fireEvent.click(screen.getByRole("tab", { name: "Jira" }));
    expect(screen.getByText("No Jira activity recorded for this delivery.")).toBeTruthy();
  });

  it("shows a Sessions tab with each session's participant and duration", async () => {
    installBackend(
      detail({
        sessions: [
          {
            id: "session-1",
            case_id: "case-1",
            execution_id: "exec-1",
            orchestration_id: "orc-1",
            participant: "codex",
            status: "closed",
            started_at: "2026-08-10T00:00:00Z",
            ended_at: "2026-08-10T01:30:00Z",
            worktree_path: "/repo/billing",
            provider: "openai",
            checkpoints: [],
          },
        ],
      }),
    );

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });
    await screen.findByRole("tab", { name: "Sessions" });

    await fireEvent.click(screen.getByRole("tab", { name: "Sessions" }));
    const panel = screen.getByRole("tabpanel", { name: "Sessions" });
    expect(within(panel).getByText("codex")).toBeTruthy();
    expect(within(panel).getByText("openai")).toBeTruthy();
  });

  it("shows an Activity tab with the merged timeline", async () => {
    installBackend(
      detail({
        activity: [{ kind: "jira", summary: "Transitioned to In Progress", occurred_at: "2026-08-10T02:00:00Z" }],
      }),
    );

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });
    await screen.findByRole("tab", { name: "Activity" });

    await fireEvent.click(screen.getByRole("tab", { name: "Activity" }));
    const panel = screen.getByRole("tabpanel", { name: "Activity" });
    expect(within(panel).getByText("Transitioned to In Progress")).toBeTruthy();
  });

  it("confirms a cancel, saying what it does and does not undo, using a freshly fetched revision", async () => {
    const posted: { url: string; body: unknown }[] = [];
    (fetch as unknown as FetchMock).mockImplementation(async (url: string, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "POST") {
        posted.push({ url, body: init?.body ? JSON.parse(init.body as string) : undefined });
        return jsonResponse(detail({ status: "cancelled", cancellable: false }));
      }
      return jsonResponse(detail({ orchestration_revision: 4 }));
    });

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });
    await waitFor(() => expect(screen.getByRole("heading", { name: "Migrate billing to v2" })).toBeTruthy());

    await fireEvent.click(screen.getByRole("button", { name: "Cancel delivery" }));
    const dialog = screen.getByRole("dialog");
    expect(dialog.textContent).toContain("does not undo work already done");

    await fireEvent.click(within(dialog).getByRole("button", { name: "Cancel delivery" }));

    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0].url).toBe("/api/v1/deliveries/orc-1/cancel");
    expect(posted[0].body).toEqual({ expected_revision: 4 });
  });

  it("surfaces a failed cancel in the dialog instead of closing it", async () => {
    installBackend(detail(), { onPost: () => jsonResponse({ error: "revision conflict" }, false, 409) });

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });
    await waitFor(() => expect(screen.getByRole("heading", { name: "Migrate billing to v2" })).toBeTruthy());

    await fireEvent.click(screen.getByRole("button", { name: "Cancel delivery" }));
    const dialog = screen.getByRole("dialog");
    await fireEvent.click(within(dialog).getByRole("button", { name: "Cancel delivery" }));

    await waitFor(() => expect(screen.getByTestId("cancel-error").textContent).toContain("revision conflict"));
    expect(screen.getByRole("dialog")).toBeTruthy();
  });

  it("does not offer Cancel once the delivery is no longer cancellable", async () => {
    installBackend(detail({ cancellable: false, status: "completed" }));

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });
    await waitFor(() => expect(screen.getByRole("heading", { name: "Migrate billing to v2" })).toBeTruthy());
    expect(screen.queryByRole("button", { name: "Cancel delivery" })).toBeNull();
  });

  it("shows an error state when the initial load fails", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse({ error: "not found" }, false, 404));

    render(DeliveryDetail, { props: { orchestrationId: "orc-1" } });

    await waitFor(() => expect(screen.getByText("not found", { exact: false })).toBeTruthy());
  });
});
