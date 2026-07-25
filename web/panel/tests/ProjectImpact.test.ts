import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectImpact from "../src/routes/projects/ProjectImpact.svelte";
import { setCsrfToken } from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

type FetchMock = ReturnType<typeof vi.fn>;

const nodesPayload = {
  items: [{ id: "svc-auth", type: "service", label: "Auth service" }],
};

const resultPayload = {
  direct_impact: [{ id: "svc-auth", type: "service", label: "Auth service" }],
  transitive_impact: [],
  affected_repositories: ["repo-auth", "repo-gateway"],
  affected_tests: [{ id: "test-login", type: "test", label: "Login test" }],
  deployment_artifacts: [],
  owners: [{ id: "team-identity", type: "team", label: "Identity team" }],
  missing_coverage: [{ id: "svc-billing", type: "service", label: "Billing" }],
  related_contradictions: ["c1"],
};

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  setCsrfToken("csrf-test-token");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ProjectImpact", () => {
  it("renders the query result as readable lists", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (url: string, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "GET" && url.includes("/impact/nodes")) return jsonResponse(nodesPayload);
      if (method === "POST" && url.includes("/impact/query")) return jsonResponse(resultPayload);
      return jsonResponse({});
    });
    render(ProjectImpact, { props: { projectId: "p1" } });

    // Enter a subject and query.
    await waitFor(() => expect(screen.getByLabelText("Impact subject")).toBeTruthy());
    await fireEvent.input(screen.getByLabelText("Impact subject"), { target: { value: "svc-auth" } });
    await fireEvent.click(screen.getByRole("button", { name: "Query" }));

    await waitFor(() => expect(screen.getByTestId("impact-result")).toBeTruthy());
    const result = within(screen.getByTestId("impact-result"));
    expect(result.getByText("repo-auth")).toBeTruthy();
    expect(result.getByText("Login test")).toBeTruthy();
    expect(result.getByText("Identity team")).toBeTruthy();
    expect(result.getByText("Billing")).toBeTruthy();
    expect(result.getByText("c1")).toBeTruthy();
  });
});
