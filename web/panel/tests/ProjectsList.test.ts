import { fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectsList from "../src/routes/projects/ProjectsList.svelte";
import { setCsrfToken } from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

type FetchMock = ReturnType<typeof vi.fn>;

function project(id: string, over: Record<string, unknown> = {}) {
  return {
    id,
    name: "",
    description: "",
    path: `/srv/${id}`,
    pinned: false,
    primary: false,
    availability: "available",
    repository_count: 1,
    knowledge_count: 0,
    active_session_count: 0,
    metadata_count: 0,
    ...over,
  };
}

const threeProjects = [
  project("checkout-svc", { name: "Checkout", description: "Payment flow" }),
  project("billing-api", { name: "Billing", description: "Invoices and dunning" }),
  project("atlas", { name: "Atlas", description: "Internal docs" }),
];

// GET serves the current list; a DELETE removes from it, so the component's
// post-delete reload reflects the removal. Tests override onDelete to force an
// error response.
function installBackend(initial: ReturnType<typeof project>[], opts: { onDelete?: () => Response } = {}) {
  const state = { items: [...initial] };
  (fetch as unknown as FetchMock).mockImplementation(async (url: string, init?: RequestInit) => {
    const method = (init?.method ?? "GET").toUpperCase();
    if (method === "GET") return jsonResponse({ items: state.items });
    if (opts.onDelete) return opts.onDelete();
    const id = decodeURIComponent(url.split("/projects/")[1] ?? "");
    state.items = state.items.filter((p) => p.id !== id);
    return { ok: true, status: 204 } as Response;
  });
  return state;
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  setCsrfToken("csrf-test-token");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ProjectsList", () => {
  it("shows the empty state when there are no projects at all", async () => {
    installBackend([]);
    render(ProjectsList);
    await waitFor(() => expect(screen.getByText("No projects yet")).toBeTruthy());
    // The search box is pointless with nothing to search, so it stays hidden.
    expect(screen.queryByLabelText("Search projects")).toBeNull();
  });

  it("shows an error state when the list fails to load", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse({ error: "boom" }, false, 500));
    render(ProjectsList);
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("boom"));
  });

  it("filters the list as the user types", async () => {
    installBackend(threeProjects);
    render(ProjectsList);
    await waitFor(() => expect(screen.getByText("Checkout")).toBeTruthy());

    await fireEvent.input(screen.getByLabelText("Search projects"), { target: { value: "dunning" } });

    await waitFor(() => expect(screen.queryByText("Checkout")).toBeNull());
    expect(screen.getByText("Billing")).toBeTruthy();
    expect(screen.queryByText("Atlas")).toBeNull();
  });

  it("distinguishes an empty search result from having no projects", async () => {
    installBackend(threeProjects);
    render(ProjectsList);
    await waitFor(() => expect(screen.getByText("Checkout")).toBeTruthy());

    await fireEvent.input(screen.getByLabelText("Search projects"), { target: { value: "zzzz" } });

    await waitFor(() => expect(screen.getByText("No projects match your search")).toBeTruthy());
    expect(screen.queryByText("No projects yet")).toBeNull();
  });

  it("reorders the list when the sort control changes", async () => {
    installBackend([
      project("a", { name: "Alpha", repository_count: 1 }),
      project("z", { name: "Zulu", repository_count: 9 }),
    ]);
    const { container } = render(ProjectsList);
    await waitFor(() => expect(screen.getByText("Alpha")).toBeTruthy());

    const names = () => Array.from(container.querySelectorAll(".name")).map((n) => n.textContent);
    expect(names()).toEqual(["Alpha", "Zulu"]);

    await fireEvent.change(screen.getByLabelText("Sort by"), { target: { value: "repositories" } });
    await waitFor(() => expect(names()).toEqual(["Zulu", "Alpha"]));
  });

  it("requires the project's name to be typed before it will delete", async () => {
    installBackend(threeProjects);
    render(ProjectsList);
    await waitFor(() => expect(screen.getByText("Checkout")).toBeTruthy());

    await fireEvent.click(screen.getByLabelText("Remove Checkout from Punakawan"));

    const confirmButton = screen.getByRole("button", { name: "Remove project" });
    expect((confirmButton as HTMLButtonElement).disabled).toBe(true);

    // A near-miss must not unlock it.
    await fireEvent.input(screen.getByLabelText(/Type/), { target: { value: "Checkou" } });
    expect((confirmButton as HTMLButtonElement).disabled).toBe(true);

    await fireEvent.input(screen.getByLabelText(/Type/), { target: { value: "Checkout" } });
    await waitFor(() => expect((confirmButton as HTMLButtonElement).disabled).toBe(false));
  });

  it("states that the repository and its files are not deleted", async () => {
    installBackend(threeProjects);
    render(ProjectsList);
    await waitFor(() => expect(screen.getByText("Checkout")).toBeTruthy());

    await fireEvent.click(screen.getByLabelText("Remove Checkout from Punakawan"));

    const dialog = screen.getByRole("dialog");
    expect(dialog.textContent).toContain("does not delete the repository");
    expect(dialog.textContent).toContain("/srv/checkout-svc");
  });

  it("accepts the project id as confirmation and removes the project", async () => {
    const state = installBackend(threeProjects);
    render(ProjectsList);
    await waitFor(() => expect(screen.getByText("Checkout")).toBeTruthy());

    await fireEvent.click(screen.getByLabelText("Remove Checkout from Punakawan"));
    await fireEvent.input(screen.getByLabelText(/Type/), { target: { value: "checkout-svc" } });
    await fireEvent.click(screen.getByRole("button", { name: "Remove project" }));

    await waitFor(() => expect(screen.queryByText("Checkout")).toBeNull());
    expect(state.items.map((p) => p.id)).toEqual(["billing-api", "atlas"]);
  });

  it("explains a 409 as the primary workspace refusing removal", async () => {
    installBackend(threeProjects, {
      onDelete: () =>
        jsonResponse({ error: "the primary workspace cannot be removed", code: "primary_project" }, false, 409),
    });
    render(ProjectsList);
    await waitFor(() => expect(screen.getByText("Checkout")).toBeTruthy());

    await fireEvent.click(screen.getByLabelText("Remove Checkout from Punakawan"));
    await fireEvent.input(screen.getByLabelText(/Type/), { target: { value: "Checkout" } });
    await fireEvent.click(screen.getByRole("button", { name: "Remove project" }));

    await waitFor(() =>
      expect(screen.getByTestId("removal-error").textContent).toContain("primary workspace"),
    );
  });

  it("offers no Remove action for the primary project", async () => {
    installBackend([project("primary-ws", { name: "Primary", primary: true })]);
    render(ProjectsList);
    await waitFor(() => expect(screen.getByText("Primary")).toBeTruthy());
    expect(screen.queryByLabelText("Remove Primary from Punakawan")).toBeNull();
  });
});
