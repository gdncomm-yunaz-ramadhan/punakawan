import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ProjectWorkflows from "../src/routes/projects/ProjectWorkflows.svelte";
import { setCsrfToken } from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

type FetchMock = ReturnType<typeof vi.fn>;

const baseWorkflow = {
  version: "1",
  id: "deploy",
  name: "Deploy",
  description: "Ship it",
  enabled: false,
  required_metadata: ["deploy_target"],
  inputs: [{ name: "branch", type: "string", required: true, default: "main" }],
  steps: [{ id: "s1", capability: "git.push" }],
  allowed_capabilities: ["git.push"],
  revision: 1,
};

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  setCsrfToken("csrf-test-token");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ProjectWorkflows", () => {
  it("lists workflows and toggles enable, POSTing to /enable and flipping the badge", async () => {
    let enabled = false;
    (fetch as unknown as FetchMock).mockImplementation(async (url: string, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "GET") {
        return jsonResponse({ items: [{ ...baseWorkflow, enabled }] });
      }
      // POST enable/disable.
      enabled = url.endsWith("/enable");
      return jsonResponse({ ...baseWorkflow, enabled });
    });

    render(ProjectWorkflows, { props: { projectId: "p1" } });
    await waitFor(() => expect(screen.getByText("Deploy")).toBeTruthy());
    expect(screen.getByText("Disabled")).toBeTruthy();

    // Toggle is now the shared Button primitive; its accessible name comes
    // from the preserved aria-label. The data-testid="toggle-deploy" span
    // still wraps it.
    await fireEvent.click(screen.getByRole("button", { name: "Enable Deploy" }));

    await waitFor(() => {
      const post = (fetch as unknown as FetchMock).mock.calls.find((c) => c[1]?.method === "POST");
      expect(post).toBeTruthy();
      expect(post![0]).toBe("/api/v1/projects/p1/workflows/deploy/enable");
    });
    await waitFor(() => expect(screen.getByText("Enabled")).toBeTruthy());
  });

  it("opens the workflow detail as a modal dialog, and closes it", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async () =>
      jsonResponse({ items: [{ ...baseWorkflow, enabled: true }] }),
    );

    render(ProjectWorkflows, { props: { projectId: "p1" } });
    await waitFor(() => expect(screen.getByText("Deploy")).toBeTruthy());
    expect(screen.queryByRole("dialog")).toBeNull();

    await fireEvent.click(screen.getByTestId("workflow-row-deploy"));
    await waitFor(() => expect(screen.getByRole("dialog")).toBeTruthy());
    expect(screen.getByTestId("workflow-detail")).toBeTruthy();

    await fireEvent.click(screen.getByRole("button", { name: "Close" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("labels each workflow step with the role that owns its capability", async () => {
    const roleWorkflow = {
      ...baseWorkflow,
      steps: [
        { id: "ctx", capability: "build_task_context", intent: "implement" },
        { id: "review", capability: "submit_bagong_review", intent: "review" },
      ],
    };
    (fetch as unknown as FetchMock).mockImplementation(async () =>
      jsonResponse({ items: [{ ...roleWorkflow, enabled: true }] }),
    );

    render(ProjectWorkflows, { props: { projectId: "p1" } });
    await waitFor(() => expect(screen.getByText("Deploy")).toBeTruthy());
    await fireEvent.click(screen.getByTestId("workflow-row-deploy"));

    // build_task_context is execution work -> Petruk; submit_bagong_review -> Bagong.
    await waitFor(() => expect(screen.getByText("Built by Petruk")).toBeTruthy());
    expect(screen.getByText("Verified by Bagong")).toBeTruthy();
  });

  it("renders the stepper with steps in order, each carrying its role label", async () => {
    const roleWorkflow = {
      ...baseWorkflow,
      steps: [
        { id: "ctx", capability: "build_task_context", intent: "implement" },
        { id: "review", capability: "submit_bagong_review", intent: "review" },
      ],
    };
    (fetch as unknown as FetchMock).mockImplementation(async () =>
      jsonResponse({ items: [{ ...roleWorkflow, enabled: true }] }),
    );

    render(ProjectWorkflows, { props: { projectId: "p1" } });
    await waitFor(() => expect(screen.getByText("Deploy")).toBeTruthy());
    await fireEvent.click(screen.getByTestId("workflow-row-deploy"));

    const stepper = await screen.findByTestId("workflow-stepper");
    const stepEls = stepper.querySelectorAll("li.step");
    // Trigger bookend, both workflow steps, and the finish bookend.
    expect(stepEls.length).toBe(4);

    const stepTexts = Array.from(stepEls).map((el) => el.textContent ?? "");
    expect(stepTexts[0]).toContain("Invoke");
    expect(stepTexts[1]).toContain("STEP 1");
    expect(stepTexts[1]).toContain("implement");
    expect(stepTexts[1]).toContain("Built by Petruk");
    expect(stepTexts[2]).toContain("STEP 2");
    expect(stepTexts[2]).toContain("review");
    expect(stepTexts[2]).toContain("Verified by Bagong");
    expect(stepTexts[3]).toContain("Complete");
  });

  it("edits the raw definition and saves it, updating the list and the modal", async () => {
    const savedWorkflow = { ...baseWorkflow, name: "Deploy v2", version: "2", enabled: true, revision: 2 };
    (fetch as unknown as FetchMock).mockImplementation(async (_url: string, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "GET") {
        return jsonResponse({ items: [{ ...baseWorkflow, enabled: true }] });
      }
      // POST /workflows - the create-or-update upsert.
      return jsonResponse(savedWorkflow);
    });

    render(ProjectWorkflows, { props: { projectId: "p1" } });
    await waitFor(() => expect(screen.getByText("Deploy")).toBeTruthy());
    await fireEvent.click(screen.getByTestId("workflow-row-deploy"));
    await waitFor(() => expect(screen.getByRole("dialog")).toBeTruthy());

    const textarea = screen.getByTestId("definition-textarea") as HTMLTextAreaElement;
    await fireEvent.input(textarea, { target: { value: JSON.stringify(savedWorkflow) } });
    await fireEvent.click(screen.getByRole("button", { name: "Save definition" }));

    await waitFor(() => {
      const post = (fetch as unknown as FetchMock).mock.calls.find((c) => c[1]?.method === "POST");
      expect(post).toBeTruthy();
      expect(post![0]).toBe("/api/v1/projects/p1/workflows");
      expect(JSON.parse(post![1].body as string)).toEqual(savedWorkflow);
    });

    // The card in the list and the modal's version both reflect the update.
    const workflowRow = screen.getByTestId("workflow-row-deploy");
    await waitFor(() => expect(within(workflowRow).getByText("Deploy v2")).toBeTruthy());
    expect(within(screen.getByTestId("workflow-detail")).getByText("v2")).toBeTruthy();
  });

  it("tells the user to reload on a revision conflict rather than showing a generic error", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (_url: string, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "GET") {
        return jsonResponse({ items: [{ ...baseWorkflow, enabled: true }] });
      }
      return jsonResponse({ code: "revision_conflict", error: "stale revision" }, false, 409);
    });

    render(ProjectWorkflows, { props: { projectId: "p1" } });
    await waitFor(() => expect(screen.getByText("Deploy")).toBeTruthy());
    await fireEvent.click(screen.getByTestId("workflow-row-deploy"));
    await waitFor(() => expect(screen.getByRole("dialog")).toBeTruthy());

    await fireEvent.click(screen.getByRole("button", { name: "Save definition" }));

    await waitFor(() =>
      expect(screen.getByTestId("save-definition-error").textContent).toContain(
        "Someone else changed this workflow",
      ),
    );
  });

  it("shows a validation error for invalid JSON without calling the API", async () => {
    const fetchMock = fetch as unknown as FetchMock;
    fetchMock.mockImplementation(async (_url: string, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "GET") {
        return jsonResponse({ items: [{ ...baseWorkflow, enabled: true }] });
      }
      throw new Error("the API should not be called for invalid JSON");
    });

    render(ProjectWorkflows, { props: { projectId: "p1" } });
    await waitFor(() => expect(screen.getByText("Deploy")).toBeTruthy());
    await fireEvent.click(screen.getByTestId("workflow-row-deploy"));
    await waitFor(() => expect(screen.getByRole("dialog")).toBeTruthy());

    const textarea = screen.getByTestId("definition-textarea") as HTMLTextAreaElement;
    await fireEvent.input(textarea, { target: { value: "{ not valid json" } });
    await fireEvent.click(screen.getByRole("button", { name: "Save definition" }));

    await waitFor(() =>
      expect(screen.getByTestId("save-definition-error").textContent).toContain("Invalid JSON"),
    );
    expect(fetchMock.mock.calls.some((c) => c[1]?.method === "POST")).toBe(false);
  });
});
