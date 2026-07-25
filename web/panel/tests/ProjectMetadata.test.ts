import { fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectMetadata from "../src/routes/projects/ProjectMetadata.svelte";
import { setCsrfToken } from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

type FetchMock = ReturnType<typeof vi.fn>;
type Entry = { key: string; description: string; value: unknown };

// A tiny in-memory backend: GET returns the current list + revision;
// mutations mutate that state so the component's post-mutation reload
// reflects the change. Individual tests override the mutation branch when
// they want to force a 409/400.
function installBackend(initial: { items: Entry[]; revision: number }, opts: { onMutate?: () => Response } = {}) {
  const state = { items: [...initial.items], revision: initial.revision };
  (fetch as unknown as FetchMock).mockImplementation(async (_url: string, init?: RequestInit) => {
    const method = (init?.method ?? "GET").toUpperCase();
    if (method === "GET") {
      return jsonResponse({ items: state.items, revision: state.revision });
    }
    if (opts.onMutate) return opts.onMutate();
    if (method === "POST") {
      const body = JSON.parse(init!.body as string) as Entry;
      state.items = [...state.items, { key: body.key, description: body.description, value: body.value }];
      state.revision += 1;
      return jsonResponse({ entry: state.items.at(-1), revision: state.revision }, true, 201);
    }
    if (method === "PATCH") {
      const body = JSON.parse(init!.body as string) as Partial<Entry>;
      const key = decodeURIComponent((_url.split("/metadata/")[1] ?? "").split("?")[0]);
      state.items = state.items.map((e) =>
        e.key === key ? { ...e, description: body.description ?? e.description, value: body.value } : e,
      );
      state.revision += 1;
      return jsonResponse({ entry: state.items.find((e) => e.key === key), revision: state.revision });
    }
    // DELETE
    const key = decodeURIComponent((_url.split("/metadata/")[1] ?? "").split("?")[0]);
    state.items = state.items.filter((e) => e.key !== key);
    state.revision += 1;
    return { ok: false, status: 204 } as Response;
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

describe("ProjectMetadata", () => {
  it("adds an entry through the confirm dialog and sends base_revision", async () => {
    installBackend({ items: [], revision: 5 });
    render(ProjectMetadata, { props: { projectId: "p1" } });

    // The add fields now live in a modal opened by the "Add metadata" button.
    await waitFor(() => expect(screen.getByTestId("add-metadata")).toBeTruthy());
    await fireEvent.click(screen.getByTestId("add-metadata"));
    await waitFor(() => expect(screen.getByLabelText("New metadata key")).toBeTruthy());

    await fireEvent.input(screen.getByLabelText("New metadata key"), { target: { value: "env" } });
    await fireEvent.input(screen.getByLabelText("New metadata description"), { target: { value: "Environment" } });
    await fireEvent.input(screen.getByLabelText("New metadata value"), { target: { value: "prod" } });

    await fireEvent.click(screen.getByRole("button", { name: "Add" }));

    // Confirm dialog shows the old->new diff before committing.
    await waitFor(() => expect(screen.getByRole("dialog")).toBeTruthy());
    expect(screen.getAllByText("(none)").length).toBeGreaterThan(0);

    await fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() => {
      const post = (fetch as unknown as FetchMock).mock.calls.find((c) => c[1]?.method === "POST");
      expect(post).toBeTruthy();
      const body = JSON.parse(post![1].body);
      expect(body.key).toBe("env");
      expect(body.value).toBe("prod");
      expect(body.base_revision).toBe(5);
    });

    // Dialog closed, new row visible after reload.
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.getByText("env")).toBeTruthy();
  });

  it("edits an entry, confirming the old->new value", async () => {
    installBackend({ items: [{ key: "env", description: "Environment", value: "prod" }], revision: 5 });
    render(ProjectMetadata, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByText("env")).toBeTruthy());

    await fireEvent.click(screen.getByRole("button", { name: "Edit env" }));
    await fireEvent.input(screen.getByLabelText("Edit value for env"), { target: { value: "staging" } });
    await fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(screen.getByRole("dialog")).toBeTruthy());
    // Diff shows old and new value.
    expect(screen.getByText("prod")).toBeTruthy();
    expect(screen.getByText("staging")).toBeTruthy();

    await fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() => {
      const patch = (fetch as unknown as FetchMock).mock.calls.find((c) => c[1]?.method === "PATCH");
      expect(patch).toBeTruthy();
      const body = JSON.parse(patch![1].body);
      expect(body.value).toBe("staging");
      expect(body.base_revision).toBe(5);
    });
    await waitFor(() => expect(screen.getByText("staging")).toBeTruthy());
  });

  it("deletes an entry after confirmation", async () => {
    installBackend({ items: [{ key: "env", description: "Environment", value: "prod" }], revision: 5 });
    render(ProjectMetadata, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByText("env")).toBeTruthy());

    await fireEvent.click(screen.getByRole("button", { name: "Delete env" }));
    await waitFor(() => expect(screen.getByRole("dialog")).toBeTruthy());
    expect(screen.getByText("(removed)")).toBeTruthy();

    await fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() => {
      const del = (fetch as unknown as FetchMock).mock.calls.find((c) => c[1]?.method === "DELETE");
      expect(del).toBeTruthy();
      expect(del![0]).toContain("base_revision=5");
    });
    await waitFor(() => expect(screen.queryByText("env")).toBeNull());
  });

  it("shows a conflict notice and reloads on 409", async () => {
    installBackend(
      { items: [], revision: 5 },
      { onMutate: () => jsonResponse({ error: "revision changed" }, false, 409) },
    );
    render(ProjectMetadata, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByTestId("add-metadata")).toBeTruthy());
    await fireEvent.click(screen.getByTestId("add-metadata"));
    await waitFor(() => expect(screen.getByLabelText("New metadata key")).toBeTruthy());
    await fireEvent.input(screen.getByLabelText("New metadata key"), { target: { value: "env" } });
    await fireEvent.click(screen.getByRole("button", { name: "Add" }));
    await waitFor(() => expect(screen.getByRole("dialog")).toBeTruthy());
    await fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() => expect(screen.getByTestId("conflict-notice")).toBeTruthy());
    // Dialog closed after the conflict; the list was reloaded.
    expect(screen.queryByRole("dialog")).toBeNull();
    const getCalls = (fetch as unknown as FetchMock).mock.calls.filter((c) => (c[1]?.method ?? "GET") === "GET");
    expect(getCalls.length).toBeGreaterThanOrEqual(2);
  });

  it("shows the validation message on a 400 duplicate_key", async () => {
    installBackend(
      { items: [], revision: 5 },
      { onMutate: () => jsonResponse({ code: "duplicate_key", error: "dup" }, false, 400) },
    );
    render(ProjectMetadata, { props: { projectId: "p1" } });

    await waitFor(() => expect(screen.getByTestId("add-metadata")).toBeTruthy());
    await fireEvent.click(screen.getByTestId("add-metadata"));
    await waitFor(() => expect(screen.getByLabelText("New metadata key")).toBeTruthy());
    await fireEvent.input(screen.getByLabelText("New metadata key"), { target: { value: "env" } });
    await fireEvent.click(screen.getByRole("button", { name: "Add" }));
    await waitFor(() => expect(screen.getByRole("dialog")).toBeTruthy());
    await fireEvent.click(screen.getByRole("button", { name: "Confirm" }));

    await waitFor(() => expect(screen.getByTestId("mutation-error").textContent).toContain("already exists"));
    // Dialog stays open so the user can correct the input.
    expect(screen.getByRole("dialog")).toBeTruthy();
  });
});
