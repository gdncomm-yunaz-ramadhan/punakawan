import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectPlans from "../src/routes/projects/ProjectPlans.svelte";
import { navigate } from "../src/lib/router/router.svelte";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

type FetchMock = ReturnType<typeof vi.fn>;

const planA = {
  id: "plan-a",
  objective: "Move checkout to payments v2",
  status: "active",
  current_revision: 3,
  project_ids: ["proj-a"],
  linked_deliveries: [{ orchestration_id: "d1", scope: "project", plan_revision: 2 }],
};

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  navigate("/projects/proj-a");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ProjectPlans", () => {
  it("lists plans with their current revision and linked-delivery count", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url.includes("/plans/")) {
        return jsonResponse({ plan: { ...planA, revision: 3 } });
      }
      return jsonResponse({ items: [planA] });
    });

    render(ProjectPlans, { props: { projectId: "proj-a" } });
    await waitFor(() => expect(screen.getByText("Move checkout to payments v2")).toBeTruthy());
    expect(screen.getByText("r3")).toBeTruthy();
    expect(screen.getByText("1")).toBeTruthy();
  });

  it("opens a plan's head revision as a modal dialog on row click", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url.includes("/plans/plan-a")) {
        expect(url).not.toContain("revision=");
        return jsonResponse({ plan: { ...planA, revision: 3 }, linked_deliveries: planA.linked_deliveries });
      }
      return jsonResponse({ items: [planA] });
    });

    render(ProjectPlans, { props: { projectId: "proj-a" } });
    await waitFor(() => expect(screen.getByTestId("plan-row-plan-a")).toBeTruthy());
    await fireEvent.click(screen.getByTestId("plan-row-plan-a"));

    const dialog = await waitFor(() => screen.getByRole("dialog"));
    await waitFor(() => expect(within(dialog).getByText("r3")).toBeTruthy());
  });

  it("opens a plan with no status or linked delivery", async () => {
    const unlinkedPlan = {
      id: "plan-unlinked",
      objective: "Prepare release notes",
      current_revision: 1,
      project_ids: ["proj-a"],
      linked_deliveries: [],
    };
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url.includes("/plans/plan-unlinked")) return jsonResponse({ plan: { ...unlinkedPlan, revision: 1 } });
      return jsonResponse({ items: [unlinkedPlan] });
    });

    render(ProjectPlans, { props: { projectId: "proj-a" } });
    await waitFor(() => expect(screen.getByText("Prepare release notes")).toBeTruthy());
    expect(screen.getByText("Not recorded")).toBeTruthy();
    expect(screen.getByText("0")).toBeTruthy();

    await fireEvent.click(screen.getByTestId("plan-row-plan-unlinked"));
    const dialog = await waitFor(() => screen.getByRole("dialog"));
    expect(within(dialog).getByText("No deliveries linked to this plan.")).toBeTruthy();
  });

  // A delivery linking revision 2 while the plan head is revision 3 must
  // render revision 2, not silently substitute the head - here reached via
  // ?plan=&revision= in the URL, exactly the shape DeliveryDetail's own
  // plans-tab link now produces.
  it("renders the exact revision named by ?revision=, not the plan's head", async () => {
    navigate("/projects/proj-a?plan=plan-a&revision=2");
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url.includes("/plans/plan-a")) {
        expect(url).toContain("revision=2");
        return jsonResponse({
          plan: { id: "plan-a", objective: "Move checkout to payments v2 (r2)", revision: 2, status: "active" },
          linked_deliveries: planA.linked_deliveries,
        });
      }
      return jsonResponse({ items: [planA] });
    });

    render(ProjectPlans, { props: { projectId: "proj-a" } });

    const dialog = await waitFor(() => screen.getByRole("dialog"));
    await waitFor(() => expect(within(dialog).getByText("r2")).toBeTruthy());
    expect(within(dialog).getByText("Move checkout to payments v2 (r2)")).toBeTruthy();
  });

  it("shows the empty state when the project has no plans", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async () => jsonResponse({ items: [] }));

    render(ProjectPlans, { props: { projectId: "proj-a" } });
    await waitFor(() => expect(screen.getByText("No plans yet")).toBeTruthy());
  });

  it("treats a malformed empty list response as an empty plan state", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async () => jsonResponse(null));

    render(ProjectPlans, { props: { projectId: "proj-a" } });
    await waitFor(() => expect(screen.getByText("No plans yet")).toBeTruthy());
  });

  it("keeps the plan list visible when loading a selected plan fails", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url.includes("/plans/plan-a")) return jsonResponse({ error: "plan unavailable" }, false, 503);
      return jsonResponse({ items: [planA] });
    });

    render(ProjectPlans, { props: { projectId: "proj-a" } });
    await waitFor(() => expect(screen.getByTestId("plan-row-plan-a")).toBeTruthy());
    await fireEvent.click(screen.getByTestId("plan-row-plan-a"));

    const dialog = await waitFor(() => screen.getByRole("dialog"));
    expect(within(dialog).getByText("Failed to load plan")).toBeTruthy();
    expect(screen.getByText("Move checkout to payments v2")).toBeTruthy();
  });

  it("shows a detail error when the selected plan payload is malformed", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (url: string) => {
      if (url.includes("/plans/plan-a")) return jsonResponse({});
      return jsonResponse({ items: [planA] });
    });

    render(ProjectPlans, { props: { projectId: "proj-a" } });
    await waitFor(() => expect(screen.getByTestId("plan-row-plan-a")).toBeTruthy());
    await fireEvent.click(screen.getByTestId("plan-row-plan-a"));

    const dialog = await waitFor(() => screen.getByRole("dialog"));
    expect(within(dialog).getByText("Failed to load plan")).toBeTruthy();
  });
});
