import { fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import GlobalSearch from "../src/routes/search/GlobalSearch.svelte";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("GlobalSearch", () => {
  it("shows fused results tagged with their workspace", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({
        items: [
          {
            workspace_id: "ws-a",
            result: { Id: "pkw:requirement/repo-a/x", Title: "Refund SLA", Summary: "s", Type: "requirement", Score: 1, Match: { Kind: "bm25" }, Explanation: ["Title BM25"], Record: {} },
            rrf_score: 0.016,
          },
        ],
      }),
    );

    render(GlobalSearch);
    const input = screen.getByPlaceholderText("Search knowledge across all workspaces");
    await fireEvent.input(input, { target: { value: "refund" } });
    await fireEvent.click(screen.getByRole("button", { name: "Search" }));

    await waitFor(() => {
      expect(screen.getByText("Refund SLA")).toBeTruthy();
    });
    expect(screen.getByText("ws-a")).toBeTruthy();
    expect(screen.getByText("Title BM25")).toBeTruthy();
  });

  it("shows the no-matches state", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ items: [] }));

    render(GlobalSearch);
    const input = screen.getByPlaceholderText("Search knowledge across all workspaces");
    await fireEvent.input(input, { target: { value: "nothing" } });
    await fireEvent.click(screen.getByRole("button", { name: "Search" }));

    await waitFor(() => {
      expect(screen.getByText("No matches.")).toBeTruthy();
    });
  });
});
