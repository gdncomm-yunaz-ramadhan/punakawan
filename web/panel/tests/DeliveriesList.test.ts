import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DeliveriesList from "../src/routes/deliveries/DeliveriesList.svelte";
import { setCsrfToken } from "../src/lib/session";
import type { DeliverySummary } from "../src/lib/api/client";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

function summary(id: string, over: Partial<DeliverySummary> = {}): DeliverySummary {
  return {
    id,
    title: id,
    status: "active",
    projects: [{ id: "proj-a", slug: "proj-a" }],
    usage: {
      input_tokens: 1200,
      output_tokens: 300,
      cache_tokens: 0,
      tool_calls: 7,
      elapsed_ms: 90_000,
      estimated_costs: { USD: 1.23 },
      pricing_complete: true,
    },
    updated_at: "2026-08-10T00:00:00Z",
    cancellable: true,
    projection_revision: 1,
    ...over,
  } as DeliverySummary;
}

type FetchMock = ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  setCsrfToken("csrf-test-token");
});

afterEach(() => {
  vi.unstubAllGlobals();
  // The tab tests write ?tab= into the url; leaving it set would decide
  // the starting tab for whichever test renders next.
  window.history.replaceState({}, "", "/deliveries");
});

// installBackend serves GET /api/v1/deliveries (the list), GET
// /api/v1/deliveries/{id} (the detail a cancel confirms against), and
// routes POSTs (the cancel) to onPost when a test supplies one.
function installBackend(
  items: DeliverySummary[],
  opts: { onPost?: (url: string) => Response; snapshotRevision?: number } = {},
) {
  (fetch as unknown as FetchMock).mockImplementation(async (url: string, init?: RequestInit) => {
    const method = (init?.method ?? "GET").toUpperCase();
    if (method === "POST") {
      if (opts.onPost) return opts.onPost(url);
      return jsonResponse({});
    }
    if (url === "/api/v1/deliveries") return jsonResponse({ items, snapshot_revision: opts.snapshotRevision ?? 1 });
    const id = url.replace("/api/v1/deliveries/", "").split("?")[0];
    const found = items.find((i) => i.id === id);
    if (found) return jsonResponse({ ...found, orchestration_revision: 1, activity: [] });
    throw new Error(`unexpected url ${url}`);
  });
}

describe("DeliveriesList", () => {
  it("renders a delivery's title, status, projects, plan, progress, usage, and updated time", async () => {
    installBackend([
      summary("orc-1", {
        title: "Migrate billing to v2",
        source: { kind: "jira", key: "PAY-1842", status: "In Progress" },
        plan: { id: "plan-1", revision: 3, objective: "Ship the v2 billing rollout" },
        progress: { percent: 40, summary: "Half the projects migrated", reported_at: "2026-08-09T00:00:00Z" },
      }),
    ]);

    const { container } = render(DeliveriesList);

    await waitFor(() => expect(screen.getByText("Migrate billing to v2")).toBeTruthy());
    // Scoped to the card: the Active/Archived toggle above the list also
    // carries the word.
    expect(within(screen.getByRole("list", { name: "Deliveries" })).getByText("active")).toBeTruthy();
    expect(screen.getByText("PAY-1842")).toBeTruthy();
    expect(screen.getByText("proj-a")).toBeTruthy();
    expect(container.textContent).toContain("1,500");
    expect(container.textContent).toContain("7");
    expect(container.textContent).toContain("1m");
    expect(container.textContent).toContain("$1.23");
    expect(container.textContent).toContain("Updated");
  });

  it("does not render aggregate metric cards above delivery results", async () => {
    installBackend([
      summary("orc-1", {
        projects: [{ id: "proj-a", slug: "proj-a" }],
        plan: { id: "plan-1", revision: 1, objective: "Ship A" },
        session: { status: "active", started_at: "2026-08-09T00:00:00Z" },
        usage: {
          input_tokens: 1000,
          output_tokens: 200,
          cache_tokens: 0,
          tool_calls: 5,
          elapsed_ms: 60_000,
          estimated_costs: { USD: 1 },
          pricing_complete: true,
        },
      }),
      summary("orc-2", {
        // Shares proj-a and plan-1 with orc-1 - dedup should count each once.
        projects: [
          { id: "proj-a", slug: "proj-a" },
          { id: "proj-b", slug: "proj-b" },
        ],
        plan: { id: "plan-1", revision: 1, objective: "Ship A" },
        usage: {
          input_tokens: 500,
          output_tokens: 300,
          cache_tokens: 0,
          tool_calls: 3,
          elapsed_ms: 30_000,
          estimated_costs: { USD: 2, EUR: 4 },
          pricing_complete: false,
        },
      }),
    ]);

    render(DeliveriesList);
    await waitFor(() => expect(screen.getByText("orc-1")).toBeTruthy());

    expect(screen.queryByText("Total cost")).toBeNull();
    expect(screen.queryByText("Plans")).toBeNull();
    expect(screen.queryByText("Sessions")).toBeNull();
    expect(screen.queryByLabelText("Cost breakdown")).toBeNull();
  });

  it("labels an ad-hoc delivery as Ad-hoc", async () => {
    installBackend([summary("orc-1", { title: "Ad-hoc cleanup", source: { kind: "adhoc" } })]);

    render(DeliveriesList);
    await waitFor(() => expect(screen.getByText("Ad-hoc cleanup")).toBeTruthy());
    expect(screen.getByText("Ad-hoc")).toBeTruthy();
  });

  it("shows unknown for a delivery with no estimated cost yet", async () => {
    installBackend([summary("orc-1", { title: "Fresh delivery", usage: { ...summary("x").usage, estimated_costs: {} } })]);

    const { container } = render(DeliveriesList);
    await waitFor(() => expect(screen.getByText("Fresh delivery")).toBeTruthy());
    expect(container.textContent).toContain("unknown");
  });

  it("never renders lane/blocked/pending-question language anywhere on the page", async () => {
    installBackend([summary("orc-1", { title: "Migrate billing" })]);

    const { container } = render(DeliveriesList);
    await waitFor(() => expect(screen.getByText("Migrate billing")).toBeTruthy());

    const text = container.textContent?.toLowerCase() ?? "";
    expect(text).not.toContain("lane");
    expect(text).not.toContain("blocked");
    expect(text).not.toContain("pending question");
    for (const el of container.querySelectorAll("[aria-label]")) {
      const label = (el.getAttribute("aria-label") ?? "").toLowerCase();
      expect(label).not.toContain("lane");
      expect(label).not.toContain("blocked");
      expect(label).not.toContain("pending question");
    }
  });

  it("shows the empty state when there are no deliveries", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse({ items: [], snapshot_revision: 0 }));

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

  it("separates running, recently completed, and archived deliveries into three tabs", async () => {
    installBackend([
      summary("orc-1", { title: "Still running", status: "active" }),
      summary("orc-2", { title: "Called off", status: "cancelled", cancellable: false }),
      summary("orc-3", {
        title: "All done",
        status: "completed",
        cancellable: false,
        // Relative to now, so the test does not silently start failing
        // once the fixed date it used to carry ages past the cutoff.
        updated_at: new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString(),
      }),
      // Completed long enough ago to have aged out of Completed.
      summary("orc-4", {
        title: "Ancient history",
        status: "completed",
        cancellable: false,
        updated_at: "2025-01-01T00:00:00Z",
      }),
    ]);

    render(DeliveriesList);
    await waitFor(() => expect(screen.getByText("Still running")).toBeTruthy());
    expect(screen.queryByText("All done")).toBeNull();
    expect(screen.queryByText("Called off")).toBeNull();

    await fireEvent.click(screen.getByRole("tab", { name: /Completed \(1\)/ }));
    expect(screen.getByText("All done")).toBeTruthy();
    expect(screen.queryByText("Still running")).toBeNull();
    expect(screen.queryByText("Ancient history")).toBeNull();

    await fireEvent.click(screen.getByRole("tab", { name: /Archived \(2\)/ }));
    expect(screen.getByText("Called off")).toBeTruthy();
    expect(screen.getByText("Ancient history")).toBeTruthy();
    expect(screen.queryByText("All done")).toBeNull();
  });

  it("restores the tab named in the url and writes the selected one back", async () => {
    window.history.replaceState({}, "", "/deliveries?tab=archived");
    installBackend([
      summary("orc-1", { title: "Still running", status: "active" }),
      summary("orc-2", { title: "Called off", status: "cancelled", cancellable: false }),
    ]);

    render(DeliveriesList);
    await waitFor(() => expect(screen.getByText("Called off")).toBeTruthy());
    expect(screen.queryByText("Still running")).toBeNull();

    // Active is the default scope, so it clears the parameter rather than
    // leaving ?tab=active on every url a reader might copy.
    await fireEvent.click(screen.getByRole("tab", { name: /Active \(1\)/ }));
    expect(new URL(window.location.href).searchParams.get("tab")).toBeNull();

    await fireEvent.click(screen.getByRole("tab", { name: /Completed \(0\)/ }));
    expect(new URL(window.location.href).searchParams.get("tab")).toBe("completed");
  });

  it("filters and distinguishes an empty result from having no deliveries", async () => {
    installBackend([
      summary("orc-1", { title: "Migrate billing" }),
      summary("orc-2", { title: "Refresh checkout" }),
    ]);

    render(DeliveriesList);
    await waitFor(() => expect(screen.getByText("Migrate billing")).toBeTruthy());

    await fireEvent.input(screen.getByLabelText("Search deliveries"), { target: { value: "checkout" } });
    await waitFor(() => expect(screen.queryByText("Migrate billing")).toBeNull());
    expect(screen.getByText("Refresh checkout")).toBeTruthy();

    await fireEvent.input(screen.getByLabelText("Search deliveries"), { target: { value: "zzzz" } });
    await waitFor(() => expect(screen.getByText("No active deliveries")).toBeTruthy());
    expect(screen.getByText(/Nothing matches/)).toBeTruthy();
    expect(screen.queryByText("No deliveries yet")).toBeNull();
  });

  it("reorders the list when the sort control changes", async () => {
    installBackend([
      summary("orc-1", { title: "Zulu", updated_at: "2026-08-20T00:00:00Z" }),
      summary("orc-2", { title: "Alpha", updated_at: "2026-08-01T00:00:00Z" }),
    ]);

    const { container } = render(DeliveriesList);
    const names = () => Array.from(container.querySelectorAll(".name")).map((n) => n.textContent);

    await waitFor(() => expect(names()).toEqual(["Zulu", "Alpha"]));

    await fireEvent.change(screen.getByLabelText("Sort by"), { target: { value: "title" } });
    await waitFor(() => expect(names()).toEqual(["Alpha", "Zulu"]));
  });

  it("offers Cancel only for a delivery still in flight", async () => {
    installBackend([
      summary("orc-active", { title: "Still running", cancellable: true }),
      summary("orc-done", { title: "All finished", status: "completed", cancellable: false }),
    ]);

    render(DeliveriesList);
    await waitFor(() => expect(screen.getByText("Still running")).toBeTruthy());

    expect(screen.getByLabelText("Cancel delivery Still running")).toBeTruthy();
    expect(screen.queryByLabelText("Cancel delivery All finished")).toBeNull();
  });

  it("confirms a cancel, saying what it does and does not undo", async () => {
    const posted: string[] = [];
    installBackend([summary("orc-1", { title: "Migrate billing" })], {
      onPost: (url) => {
        posted.push(url);
        return jsonResponse({});
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
    installBackend([summary("orc-1", { title: "Migrate billing" })], {
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
