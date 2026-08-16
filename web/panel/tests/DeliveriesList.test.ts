import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DeliveriesList from "../src/routes/deliveries/DeliveriesList.svelte";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

function orchestration(id: string, status = "active") {
  return {
    id,
    revision: 1,
    status,
    unresolved_inputs: [],
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-10T00:00:00Z",
  };
}

function deliveryView(id: string) {
  return {
    orchestration: orchestration(id),
    projects: [
      { project_id: "proj-a", lane_ids: ["lane-1"], counts_by_status: { runnable: 1 } },
      { project_id: "proj-b", lane_ids: ["lane-2"], counts_by_status: { blocked: 1 } },
    ],
    lanes: [
      { lane_id: "lane-1", project_id: "proj-a", status: "runnable" },
      { lane_id: "lane-2", project_id: "proj-b", status: "blocked", blocked_by: ["lane-1"] },
    ],
    blockers: [{ lane_id: "lane-2", blocked_by: ["lane-1"] }],
    pending_approvals: [],
    pending_questions: [],
    next_action: "Wait for lane-1 to finish before lane-2 can start.",
    latest_seq: 3,
    newly_runnable_lane_ids: [],
  };
}

type FetchMock = ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("DeliveriesList", () => {
  it("renders every orchestration with its project/lane rollup and next action", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url === "/api/v1/deliveries") return jsonResponse({ items: [orchestration("orc-1")] });
      if (url === "/api/v1/deliveries/orc-1") return jsonResponse(deliveryView("orc-1"));
      throw new Error(`unexpected url ${url}`);
    });

    const { container } = render(DeliveriesList);

    await waitFor(() => expect(screen.getByText("orc-1")).toBeTruthy());
    expect(screen.getByText("Wait for lane-1 to finish before lane-2 can start.")).toBeTruthy();
    expect(container.textContent).toContain("2 projects");
    expect(container.textContent).toContain("2 lanes");
    expect(container.textContent).toContain("1 blocked");
  });

  it("shows the empty state when there are no deliveries", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse({ items: [] }));

    render(DeliveriesList);

    await waitFor(() => {
      expect(screen.getByText("No deliveries yet")).toBeTruthy();
    });
  });

  it("shows an error state when the API call fails", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse({ error: "boom" }, false, 500));

    render(DeliveriesList);

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toContain("boom");
    });
  });
});
