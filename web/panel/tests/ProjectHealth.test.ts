import { fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectHealth from "../src/routes/projects/ProjectHealth.svelte";
import { setCsrfToken } from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

type FetchMock = ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  setCsrfToken("csrf-test-token");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ProjectHealth", () => {
  it("renders the source health table and a stale indicator", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(
      jsonResponse({
        health: [{ source: "jira", availability: "available", message: "ok", checked_at: "2026-07-24T10:00:00Z" }],
        stale: true,
      }),
    );
    render(ProjectHealth, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByText("jira")).toBeTruthy());
    expect(screen.getByTestId("stale-indicator")).toBeTruthy();
  });

  it("refreshes on button click, POSTing to /health/refresh and clearing stale", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (_url: string, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "GET") {
        return jsonResponse({
          health: [{ source: "jira", availability: "busy", checked_at: "2026-07-24T10:00:00Z" }],
          stale: true,
        });
      }
      // POST /health/refresh -> fresh snapshot.
      return jsonResponse({
        health: [{ source: "jira", availability: "available", checked_at: "2026-07-24T11:00:00Z" }],
        stale: false,
      });
    });

    render(ProjectHealth, { props: { projectId: "p1" } });
    await waitFor(() => expect(screen.getByTestId("stale-indicator")).toBeTruthy());

    await fireEvent.click(screen.getByTestId("refresh-health"));

    await waitFor(() => {
      const post = (fetch as unknown as FetchMock).mock.calls.find((c) => c[1]?.method === "POST");
      expect(post).toBeTruthy();
      expect(post![0]).toBe("/api/v1/projects/p1/health/refresh");
    });
    await waitFor(() => expect(screen.getByTestId("fresh-indicator")).toBeTruthy());
    expect(screen.queryByTestId("stale-indicator")).toBeNull();
  });

  it("keeps the table and shows an error when refresh fails", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (_url: string, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "GET") {
        return jsonResponse({
          health: [{ source: "jira", availability: "available", checked_at: "2026-07-24T10:00:00Z" }],
          stale: false,
        });
      }
      return jsonResponse({ error: "probe failed" }, false, 500);
    });

    render(ProjectHealth, { props: { projectId: "p1" } });
    await waitFor(() => expect(screen.getByText("jira")).toBeTruthy());

    await fireEvent.click(screen.getByTestId("refresh-health"));

    await waitFor(() => expect(screen.getByTestId("refresh-error").textContent).toContain("probe failed"));
    // Table still on screen after a failed refresh.
    expect(screen.getByText("jira")).toBeTruthy();
  });
});
