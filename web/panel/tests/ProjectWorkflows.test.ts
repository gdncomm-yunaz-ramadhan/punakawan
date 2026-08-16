import { fireEvent, render, screen, waitFor } from "@testing-library/svelte";
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

  it("surfaces the not-yet-wired invoke message rather than hiding the button", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (_url: string, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "GET") {
        return jsonResponse({ items: [{ ...baseWorkflow, enabled: true }] });
      }
      // invoke -> backend not connected to the run engine yet.
      return jsonResponse({ error: "not connected to the run engine" }, false, 503);
    });

    render(ProjectWorkflows, { props: { projectId: "p1" } });
    await waitFor(() => expect(screen.getByText("Deploy")).toBeTruthy());

    // Expand the row to reveal the invoke form.
    await fireEvent.click(screen.getByTestId("workflow-row-deploy"));
    await waitFor(() => expect(screen.getByTestId("invoke-btn")).toBeTruthy());

    await fireEvent.click(screen.getByRole("button", { name: "Execute workflow" }));

    await waitFor(() =>
      expect(screen.getByTestId("invoke-error").textContent).toContain("not connected to the run engine"),
    );
    // Button remains available.
    expect(screen.getByTestId("invoke-btn")).toBeTruthy();
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

  it("shows a disabled-workflow message on a 409 invoke", async () => {
    (fetch as unknown as FetchMock).mockImplementation(async (_url: string, init?: RequestInit) => {
      const method = (init?.method ?? "GET").toUpperCase();
      if (method === "GET") {
        return jsonResponse({ items: [{ ...baseWorkflow, enabled: false }] });
      }
      return jsonResponse({ code: "disabled", error: "workflow disabled" }, false, 409);
    });

    render(ProjectWorkflows, { props: { projectId: "p1" } });
    await waitFor(() => expect(screen.getByText("Deploy")).toBeTruthy());

    await fireEvent.click(screen.getByTestId("workflow-row-deploy"));
    await waitFor(() => expect(screen.getByTestId("invoke-btn")).toBeTruthy());
    await fireEvent.click(screen.getByRole("button", { name: "Execute workflow" }));

    await waitFor(() => expect(screen.getByTestId("invoke-error").textContent).toContain("enable it before invoking"));
  });
});
