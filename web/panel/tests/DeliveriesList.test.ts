import { fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DeliveriesList from "../src/routes/deliveries/DeliveriesList.svelte";
import { setCsrfToken } from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

function orchestration(id: string, status = "active", over: Record<string, unknown> = {}) {
  return {
    id,
    revision: 1,
    status,
    unresolved_inputs: [],
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-10T00:00:00Z",
    ...over,
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
    pending_questions: [],
    next_action: "Wait for lane-1 to finish before lane-2 can start.",
    latest_seq: 3,
    newly_runnable_lane_ids: [],
  };
}

type FetchMock = ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  setCsrfToken("csrf-test-token");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

// Serves a list plus a per-orchestration view, so the cards render their
// rollup. POSTs (the cancel) are routed to onPost when a test supplies one.
function installBackend(
  items: ReturnType<typeof orchestration>[],
  views: Record<string, unknown> = {},
  opts: { onPost?: (url: string) => Response } = {},
) {
  (fetch as unknown as FetchMock).mockImplementation(async (url: string, init?: RequestInit) => {
    const method = (init?.method ?? "GET").toUpperCase();
    if (method === "POST") {
      if (opts.onPost) return opts.onPost(url);
      return jsonResponse({});
    }
    if (url === "/api/v1/deliveries") return jsonResponse({ items });
    const id = url.replace("/api/v1/deliveries/", "").split("?")[0];
    if (views[id]) return jsonResponse(views[id]);
    if (items.some((o) => o.id === id)) return jsonResponse(deliveryView(id));
    throw new Error(`unexpected url ${url}`);
  });
}

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

  it("renders the backend title as the heading and keeps the id as muted text", async () => {
    const rawId = "c642fb0e8a69bdcae2be77e3ab";
    installBackend([orchestration(rawId, "active", { title: "Migrate billing to v2" })], {
      [rawId]: deliveryView(rawId),
    });

    const { container } = render(DeliveriesList);

    await waitFor(() => expect(screen.getByText("Migrate billing to v2")).toBeTruthy());
    expect(container.querySelector(".name")?.textContent).toBe("Migrate billing to v2");
    expect(container.querySelector(".id")?.textContent).toBe(rawId);
  });

  it("leads with the view's title when only the view carries one, keeping the id as muted text", async () => {
    const rawId = "c642fb0e8a69bdcae2be77e3ab";
    const view = { ...deliveryView(rawId), title: "Retire the legacy pricing endpoint" };
    installBackend([orchestration(rawId)], { [rawId]: view });

    const { container } = render(DeliveriesList);

    await waitFor(() =>
      expect(container.querySelector(".name")?.textContent).toBe("Retire the legacy pricing endpoint"),
    );
    expect(container.querySelector(".id")?.textContent).toBe(rawId);
  });

  it("derives a readable heading from the view when neither the record nor the view has a title", async () => {
    const rawId = "c642fb0e8a69bdcae2be77e3ab";
    const view = {
      ...deliveryView(rawId),
      lanes: [
        { lane_id: "lane-1", project_id: "proj-a", status: "runnable", parent_task_id: "PUN-42" },
        { lane_id: "lane-2", project_id: "proj-b", status: "blocked", parent_task_id: "PUN-43" },
      ],
    };
    installBackend([orchestration(rawId)], { [rawId]: view });

    const { container } = render(DeliveriesList);

    await waitFor(() => expect(container.querySelector(".name")?.textContent).toBe("PUN-42, PUN-43"));
    expect(container.querySelector(".id")?.textContent).toBe(rawId);
  });

  it("filters and distinguishes an empty result from having no deliveries", async () => {
    installBackend([
      orchestration("orc-1", "active", { title: "Migrate billing" }),
      orchestration("orc-2", "active", { title: "Refresh checkout" }),
    ]);

    render(DeliveriesList);
    await waitFor(() => expect(screen.getByText("Migrate billing")).toBeTruthy());

    await fireEvent.input(screen.getByLabelText("Search deliveries"), { target: { value: "checkout" } });
    await waitFor(() => expect(screen.queryByText("Migrate billing")).toBeNull());
    expect(screen.getByText("Refresh checkout")).toBeTruthy();

    await fireEvent.input(screen.getByLabelText("Search deliveries"), { target: { value: "zzzz" } });
    await waitFor(() => expect(screen.getByText("No deliveries match your search")).toBeTruthy());
    expect(screen.queryByText("No deliveries yet")).toBeNull();
  });

  it("reorders the list when the sort control changes", async () => {
    installBackend([
      orchestration("orc-1", "active", { title: "Zulu", updated_at: "2026-08-20T00:00:00Z" }),
      orchestration("orc-2", "active", { title: "Alpha", updated_at: "2026-08-01T00:00:00Z" }),
    ]);

    const { container } = render(DeliveriesList);
    const names = () => Array.from(container.querySelectorAll(".name")).map((n) => n.textContent);

    await waitFor(() => expect(names()).toEqual(["Zulu", "Alpha"]));

    await fireEvent.change(screen.getByLabelText("Sort by"), { target: { value: "title" } });
    await waitFor(() => expect(names()).toEqual(["Alpha", "Zulu"]));
  });

  it("offers Cancel only for a delivery still in flight", async () => {
    installBackend([
      orchestration("orc-active", "active", { title: "Still running" }),
      orchestration("orc-done", "completed", { title: "All finished" }),
    ]);

    render(DeliveriesList);
    await waitFor(() => expect(screen.getByText("Still running")).toBeTruthy());

    expect(screen.getByLabelText("Cancel delivery Still running")).toBeTruthy();
    expect(screen.queryByLabelText("Cancel delivery All finished")).toBeNull();
  });

  it("confirms a cancel, saying what it does and does not undo", async () => {
    const posted: string[] = [];
    installBackend([orchestration("orc-1", "active", { title: "Migrate billing" })], {}, {
      onPost: (url) => {
        posted.push(url);
        return jsonResponse(deliveryView("orc-1"));
      },
    });

    render(DeliveriesList);
    await waitFor(() => expect(screen.getByText("Migrate billing")).toBeTruthy());

    await fireEvent.click(screen.getByLabelText("Cancel delivery Migrate billing"));

    const dialog = screen.getByRole("dialog");
    expect(dialog.textContent).toContain("does not undo work already done");

    await fireEvent.click(screen.getByRole("button", { name: "Cancel delivery" }));

    await waitFor(() => expect(posted).toEqual(["/api/v1/deliveries/orc-1/cancel"]));
  });

  it("surfaces a failed cancel in the dialog instead of closing it", async () => {
    installBackend([orchestration("orc-1", "active", { title: "Migrate billing" })], {}, {
      onPost: () => jsonResponse({ error: "revision conflict" }, false, 409),
    });

    render(DeliveriesList);
    await waitFor(() => expect(screen.getByText("Migrate billing")).toBeTruthy());

    await fireEvent.click(screen.getByLabelText("Cancel delivery Migrate billing"));
    await fireEvent.click(screen.getByRole("button", { name: "Cancel delivery" }));

    await waitFor(() => expect(screen.getByTestId("cancel-error").textContent).toContain("revision conflict"));
    expect(screen.getByRole("dialog")).toBeTruthy();
  });
});
