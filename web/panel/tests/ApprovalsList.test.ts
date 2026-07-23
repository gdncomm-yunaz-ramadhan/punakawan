import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ApprovalsList from "../src/routes/approvals/ApprovalsList.svelte";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ApprovalsList", () => {
  it("shows a pending approval with its CLI resolution commands", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({
        items: [
          {
            id: "appr-1",
            run_id: "run-1",
            operation: "git_push",
            requested_by: "petruk",
            status: "pending",
            created_at: "2026-07-23T00:00:00Z",
            approve_command: "punakawan approvals approve appr-1 --by <your-name>",
            deny_command: "punakawan approvals deny appr-1 --by <your-name>",
          },
        ],
      }),
    );

    render(ApprovalsList, { props: { workspaceId: "ws-a" } });

    await waitFor(() => {
      expect(screen.getByText("git_push")).toBeTruthy();
    });
    expect(screen.getByText("punakawan approvals approve appr-1 --by <your-name>")).toBeTruthy();
    expect(screen.getByText("punakawan approvals deny appr-1 --by <your-name>")).toBeTruthy();
  });

  it("shows the empty state when nothing matches", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ items: [] }));

    render(ApprovalsList, { props: { workspaceId: "ws-a" } });

    await waitFor(() => {
      expect(screen.getByText("No approval requests match these filters.")).toBeTruthy();
    });
  });

  it("shows an error state when the API call fails", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ error: "boom" }, false, 500));

    render(ApprovalsList, { props: { workspaceId: "ws-a" } });

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toContain("boom");
    });
  });
});
