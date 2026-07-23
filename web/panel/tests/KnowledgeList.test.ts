import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import KnowledgeList from "../src/routes/knowledge/KnowledgeList.svelte";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

const record = {
  id: "pkw:requirement/repo-a/refund-sla",
  type: "requirement",
  status: "active",
  title: "Refund SLA policy",
  summary: "Refunds must be processed within 5 business days.",
  source: { provider: "manual", retrieved_at: "2026-07-23T00:00:00Z" },
  extraction: { method: "manual" },
  validity: { state: "verified" },
  scope: { repository: "repo-a" },
};

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("KnowledgeList", () => {
  it("lists knowledge records with their validity badge", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ items: [record] }));

    render(KnowledgeList, { props: { workspaceId: "ws-a" } });

    await waitFor(() => {
      expect(screen.getByText("Refund SLA policy")).toBeTruthy();
    });
    expect(screen.getAllByText("Verified").length).toBeGreaterThan(0);
  });

  it("renders a search result's match explanation when a query is active", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({ items: [{ Id: record.id, Title: record.title, Summary: record.summary, Type: record.type, Score: 12, Match: { Kind: "bm25" }, Explanation: ["Title BM25"], Record: record }] }),
    );

    render(KnowledgeList, { props: { workspaceId: "ws-a" } });

    await waitFor(() => {
      expect(screen.getByText("Refund SLA policy")).toBeTruthy();
    });
    expect(screen.getByText("Title BM25")).toBeTruthy();
  });

  it("shows the empty state when nothing matches", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ items: [] }));

    render(KnowledgeList, { props: { workspaceId: "ws-a" } });

    await waitFor(() => {
      expect(screen.getByText("No knowledge records match these filters.")).toBeTruthy();
    });
  });

  it("shows an error state when the API call fails", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ error: "boom" }, false, 500));

    render(KnowledgeList, { props: { workspaceId: "ws-a" } });

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toContain("boom");
    });
  });
});
