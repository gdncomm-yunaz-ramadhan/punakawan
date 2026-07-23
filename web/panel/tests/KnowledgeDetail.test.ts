import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import KnowledgeDetail from "../src/routes/knowledge/KnowledgeDetail.svelte";

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
  validity: { state: "verified", verified_by: ["semar"] },
  relations: [{ target: "pkw:claim/repo-a/refund-claim", type: "validates" }],
};

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("KnowledgeDetail", () => {
  it("renders provenance, relations, and history", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockImplementation((url: string) => {
      if (url.includes("/relations")) return Promise.resolve(jsonResponse({ items: [] }));
      if (url.includes("/history"))
        return Promise.resolve(
          jsonResponse({ items: [{ type: "put", record_id: record.id, record_type: "requirement", timestamp: "2026-07-23T00:00:00Z" }] }),
        );
      return Promise.resolve(jsonResponse(record));
    });

    render(KnowledgeDetail, { props: { workspaceId: "ws-a", knowledgeId: record.id } });

    await waitFor(() => {
      expect(screen.getByText("Refund SLA policy")).toBeTruthy();
    });
    expect(screen.getAllByText("manual").length).toBeGreaterThan(0);
    expect(screen.getByText("pkw:claim/repo-a/refund-claim")).toBeTruthy();
    expect(screen.getByText("Created or updated")).toBeTruthy();
  });

  it("renders a retrieval_recipe's capability/intent identity, selector, and state badge", async () => {
    const recipeRecord = {
      id: "pkw:recipe/affiliate-api/jira-next-sprint",
      type: "retrieval_recipe",
      status: "active",
      title: "Next sprint issues",
      source: { provider: "user_instruction", retrieved_at: "2026-07-23T00:00:00Z" },
      extraction: { method: "manual" },
      validity: { state: "verified", verified_by: ["user"] },
      retrieval_recipe: {
        capability: "jira.issue.search",
        intent: "project.next-sprint.issues",
        provider: "jira",
        resource: "issue",
        operation: "search",
        read_only: true,
        recipe_version: 2,
        selector: {
          all: [{ field: "project", operator: "equals", value: { literal: "AFF" } }],
        },
        output: { entity_type: "jira_issue", identity_field: "key", fields: ["key", "summary"] },
        last_execution: { status: "success", result_count: 12, executed_at: "2026-07-20T00:00:00Z" },
      },
    };

    (fetch as unknown as ReturnType<typeof vi.fn>).mockImplementation((url: string) => {
      if (url.includes("/relations")) return Promise.resolve(jsonResponse({ items: [] }));
      if (url.includes("/history")) return Promise.resolve(jsonResponse({ items: [] }));
      return Promise.resolve(jsonResponse(recipeRecord));
    });

    render(KnowledgeDetail, { props: { workspaceId: "ws-a", knowledgeId: recipeRecord.id } });

    await waitFor(() => {
      expect(screen.getByText("Next sprint issues")).toBeTruthy();
    });
    expect(screen.getByText("jira.issue.search")).toBeTruthy();
    expect(screen.getByText("project.next-sprint.issues")).toBeTruthy();
    expect(screen.getByText("Verified")).toBeTruthy();
    expect(screen.getByTestId("start-recipe-review")).toBeTruthy();
    // Selector clause rendering (capability/intent are the identity; the
    // selector is the compiled condition it resolves to).
    expect(screen.getByText("project")).toBeTruthy();
    expect(screen.getByText(/12/)).toBeTruthy();
  });

  it("shows an error state when the record fails to load", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ error: "not found" }, false, 404));

    render(KnowledgeDetail, { props: { workspaceId: "ws-a", knowledgeId: "no-such-id" } });

    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toContain("not found");
    });
  });
});
