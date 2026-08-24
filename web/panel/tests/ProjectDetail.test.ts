import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectDetail from "../src/routes/projects/ProjectDetail.svelte";

// jsdom has no EventSource, so the real module never connects and there is no
// way to push a frame at a mounted view. Mocking sse.svelte's small public
// surface lets these tests hand a synthetic frame straight to whatever
// listener the component registered through onPanelEvent, exactly as the real
// module would once an SSE frame arrived.
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

function emitPanelEvent(type: string, data: unknown = { id: "evt-1", type }) {
  const evt = { type, data: JSON.stringify(data) } as unknown as MessageEvent;
  for (const cb of [...panelListeners]) cb(evt);
}

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

type FetchMock = ReturnType<typeof vi.fn>;

function detail(over: Record<string, unknown> = {}) {
  return {
    id: "proj-a",
    name: "Checkout",
    description: "Payment flow",
    path: "/srv/proj-a",
    pinned: false,
    primary: false,
    availability: "available",
    repository_count: 1,
    knowledge_count: 0,
    active_session_count: 3,
    metadata_count: 0,
    metadata: [],
    revision: 1,
    ...over,
  };
}

function projectCalls() {
  return (fetch as unknown as FetchMock).mock.calls.filter((c) => String(c[0]) === "/api/v1/projects/proj-a");
}

function activeSessionCount(): string {
  const label = screen.getByText("Active sessions");
  return (label.closest(".metric") as HTMLElement).querySelector(".value")!.textContent ?? "";
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  panelListeners = [];
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ProjectDetail", () => {
  it("fetches the project exactly once when mounted", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse(detail()));

    render(ProjectDetail, { props: { projectId: "proj-a" } });

    await waitFor(() => expect(screen.getByRole("heading", { name: "Checkout" })).toBeTruthy());
    // Both onMount and the $effect used to load, so opening a project fired
    // the same request twice (three times counting an unfiltered SSE event).
    expect(projectCalls()).toHaveLength(1);

    // Nothing arrives later either, once the effect has settled.
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(projectCalls()).toHaveLength(1);
  });

  it("ignores a panel event that cannot change anything it renders", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse(detail()));

    render(ProjectDetail, { props: { projectId: "proj-a" } });
    await waitFor(() => expect(screen.getByRole("heading", { name: "Checkout" })).toBeTruthy());
    expect(panelListeners.length).toBeGreaterThan(0);

    emitPanelEvent("session.progress", { id: "evt-2", type: "session.progress", entity_id: "sess-1" });
    emitPanelEvent("approval.requested", { id: "evt-3", type: "approval.requested" });
    emitPanelEvent("adapter.health_changed", { id: "evt-4", type: "adapter.health_changed" });

    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(projectCalls()).toHaveLength(1);
  });

  it("refreshes on a relevant event without falling back to the loading state", async () => {
    (fetch as unknown as FetchMock)
      .mockResolvedValueOnce(jsonResponse(detail()))
      .mockResolvedValue(jsonResponse(detail({ active_session_count: 9 })));

    render(ProjectDetail, { props: { projectId: "proj-a" } });
    await waitFor(() => expect(screen.getByRole("heading", { name: "Checkout" })).toBeTruthy());
    expect(activeSessionCount()).toBe("3");

    emitPanelEvent("session.started", { id: "evt-5", type: "session.started", entity_id: "run-1" });

    // The refreshed count arrives and the header stays on screen throughout -
    // a background refresh must never unmount the view behind a spinner.
    await waitFor(() => expect(activeSessionCount()).toBe("9"));
    expect(projectCalls()).toHaveLength(2);
    expect(screen.queryByText("Loading…")).toBeNull();
    expect(screen.getByRole("heading", { name: "Checkout" })).toBeTruthy();
  });

  it("keeps the spinner on a genuine first load and clears it when the request fails", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse({ error: "boom" }, false, 500));

    render(ProjectDetail, { props: { projectId: "proj-a" } });

    expect(screen.getByText("Loading…")).toBeTruthy();
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("boom"));
    expect(screen.queryByText("Loading…")).toBeNull();
  });
});
