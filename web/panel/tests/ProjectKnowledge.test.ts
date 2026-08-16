import { fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectKnowledge from "../src/routes/projects/ProjectKnowledge.svelte";

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

type FetchMock = ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ProjectKnowledge", () => {
  it("lists knowledge records with their validity badge", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse({ items: [record] }));

    render(ProjectKnowledge, { props: { projectId: "proj-a" } });

    await waitFor(() => {
      expect(screen.getByText("Refund SLA policy")).toBeTruthy();
    });
    expect(screen.getAllByText("Verified").length).toBeGreaterThan(0);
  });

  it("expands a row inline with provenance, relations, and history", async () => {
    (fetch as unknown as FetchMock).mockImplementation((url: string) => {
      if (url.includes("/relations")) return Promise.resolve(jsonResponse({ items: [] }));
      if (url.includes("/history"))
        return Promise.resolve(
          jsonResponse({ items: [{ type: "put", record_id: record.id, record_type: "requirement", timestamp: "2026-07-23T00:00:00Z" }] }),
        );
      if (/\/knowledge$/.test(url)) return Promise.resolve(jsonResponse({ items: [record] }));
      return Promise.resolve(jsonResponse(record));
    });

    render(ProjectKnowledge, { props: { projectId: "proj-a" } });
    await waitFor(() => expect(screen.getByText("Refund SLA policy")).toBeTruthy());

    await fireEvent.click(screen.getByText("Refund SLA policy"));

    await waitFor(() => {
      expect(screen.getByText("pkw:claim/repo-a/refund-claim")).toBeTruthy();
    });
    expect(screen.getByText("Created or updated")).toBeTruthy();
  });

  it("renders a retrieval_recipe's identity, selector, execution evidence, and lineage inline", async () => {
    (fetch as unknown as FetchMock).mockImplementation((url: string) => {
      if (url.includes("/relations")) return Promise.resolve(jsonResponse({ items: [] }));
      if (url.includes("/history")) return Promise.resolve(jsonResponse({ items: [] }));
      if (/\/knowledge$/.test(url)) return Promise.resolve(jsonResponse({ items: [recipeRecord] }));
      return Promise.resolve(jsonResponse(recipeRecord));
    });

    render(ProjectKnowledge, { props: { projectId: "proj-a" } });
    await waitFor(() => expect(screen.getByText("Next sprint issues")).toBeTruthy());

    await fireEvent.click(screen.getByText("Next sprint issues"));

    await waitFor(() => {
      expect(screen.getByText("jira.issue.search")).toBeTruthy();
    });
    expect(screen.getByText("project.next-sprint.issues")).toBeTruthy();
    expect(screen.getByText("project")).toBeTruthy();
    expect(screen.getByText(/12/)).toBeTruthy();
    expect(screen.getByText(/No prior or later version is known/)).toBeTruthy();
  });

  it("shows the empty state when nothing matches", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse({ items: [] }));

    render(ProjectKnowledge, { props: { projectId: "proj-a" } });

    await waitFor(() => {
      expect(screen.getByText("No knowledge records match these filters.")).toBeTruthy();
    });
  });

  it("shows an error state when the API call fails", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse({ error: "boom" }, false, 500));

    render(ProjectKnowledge, { props: { projectId: "proj-a" } });

    await waitFor(() => {
      expect(screen.getByText(/Failed to load knowledge/)).toBeTruthy();
      expect(screen.getByText(/boom/)).toBeTruthy();
    });
  });
});
