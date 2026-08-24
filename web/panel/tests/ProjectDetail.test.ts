import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectDetail from "../src/routes/projects/ProjectDetail.svelte";

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

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ProjectDetail", () => {
  it("fetches the project exactly once when mounted", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse(detail()));

    render(ProjectDetail, { props: { projectId: "proj-a" } });

    await waitFor(() => expect(screen.getByRole("heading", { name: "Checkout" })).toBeTruthy());
    // Opening a project must not duplicate its initial request.
    expect(projectCalls()).toHaveLength(1);

    // Nothing arrives later either, once the effect has settled.
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(projectCalls()).toHaveLength(1);
  });

  it("keeps the spinner on a genuine first load and clears it when the request fails", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse({ error: "boom" }, false, 500));

    render(ProjectDetail, { props: { projectId: "proj-a" } });

    expect(screen.getByText("Loading…")).toBeTruthy();
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("boom"));
    expect(screen.queryByText("Loading…")).toBeNull();
  });
});
