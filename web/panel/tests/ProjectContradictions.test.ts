import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectContradictions from "../src/routes/projects/ProjectContradictions.svelte";
import { setCsrfToken } from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

type FetchMock = ReturnType<typeof vi.fn>;

const listPayload = {
  items: [
    {
      id: "c1",
      title: "Deploy target mismatch",
      severity: "critical",
      status: "detected",
      blocking: true,
      subject: { type: "metadata", key: "deploy_target" },
      claims: [
        { source: { type: "doc", ref: "README.md" }, statement: "Deploys to production" },
        { source: { type: "config", ref: "ci.yml" }, statement: "Deploys to staging" },
      ],
      updated_at: "2026-07-20T10:00:00Z",
    },
  ],
};

const detailPayload = {
  ...listPayload.items[0],
  claims: [
    { source: { type: "doc", ref: "README.md" }, statement: "Deploys to production", evidence: ["ev-1"] },
    { source: { type: "config", ref: "ci.yml" }, statement: "Deploys to staging" },
  ],
  resolution: { proposed_statement: "Production is the canonical target", rationale: "README is authoritative" },
};

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  setCsrfToken("csrf-test-token");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ProjectContradictions", () => {
  it("renders a list row per contradiction", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse(listPayload));
    render(ProjectContradictions, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByTestId("contradiction-row-c1")).toBeTruthy());
    expect(screen.getByText("Deploy target mismatch")).toBeTruthy();
  });

  it("opens the detail with side-by-side claims on row click", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url.includes("/contradictions/c1")) return jsonResponse(detailPayload);
      return jsonResponse(listPayload);
    });
    render(ProjectContradictions, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByTestId("contradiction-row-c1")).toBeTruthy());
    await fireEvent.click(screen.getByTestId("contradiction-row-c1"));

    await waitFor(() => expect(screen.getByTestId("contradiction-detail")).toBeTruthy());
    const detail = within(screen.getByTestId("contradiction-detail"));
    expect(detail.getByText("Deploys to production")).toBeTruthy();
    expect(detail.getByText("Deploys to staging")).toBeTruthy();
    // Resolve action is available for a non-terminal status.
    expect(detail.getByRole("button", { name: "Resolve" })).toBeTruthy();
  });
});
