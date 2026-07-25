import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectDossiers from "../src/routes/projects/ProjectDossiers.svelte";
import { setCsrfToken } from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

type FetchMock = ReturnType<typeof vi.fn>;

const listPayload = {
  items: [
    {
      id: "d1",
      title: "Add OAuth login",
      status: "draft",
      objective: { statement: "Support OAuth sign-in" },
      requirements: { covered: 3, uncovered: 1 },
      contradictions: { resolved: 2, unresolved: 0 },
      plan_conformance: { implemented: 4, partial: 1, missing: 0 },
      claims: ["Login works", "Tokens rotate"],
    },
  ],
};

const detailPayload = {
  ...listPayload.items[0],
  evidence: ["ev-1"],
};

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  setCsrfToken("csrf-test-token");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ProjectDossiers", () => {
  it("renders a list row with summary indicators", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse(listPayload));
    render(ProjectDossiers, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByTestId("dossier-row-d1")).toBeTruthy());
    const row = within(screen.getByTestId("dossier-row-d1"));
    expect(row.getByText("Add OAuth login")).toBeTruthy();
    // Requirements covered 3/4 (covered + uncovered).
    expect(row.getByText("3/4")).toBeTruthy();
  });

  it("opens the detail with a Finalize and Export button", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url.includes("/dossiers/d1")) return jsonResponse(detailPayload);
      return jsonResponse(listPayload);
    });
    render(ProjectDossiers, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByTestId("dossier-row-d1")).toBeTruthy());
    await fireEvent.click(screen.getByTestId("dossier-row-d1"));

    await waitFor(() => expect(screen.getByTestId("dossier-detail")).toBeTruthy());
    expect(screen.getByTestId("finalize-dossier")).toBeTruthy();
    expect(screen.getByTestId("export-dossier")).toBeTruthy();
  });
});
