import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ConnectorsPage from "../src/routes/connectors/ConnectorsPage.svelte";
import type { Connectors } from "../src/lib/api/client";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

function connectors(over: Partial<Connectors> = {}): Connectors {
  return {
    credentials_path: "/home/someone/.config/punakawan/credentials.yaml",
    adapters: [
      {
        id: "atlassian",
        label: "Jira",
        provider: "jira",
        command: "node",
        entrypoint: "/opt/punakawan/adapters/atlassian/dist/run.js",
        installed: true,
        organizations: [
          {
            id: "gdncomm",
            adapter_id: "atlassian:gdncomm",
            base_url: "https://gdncomm.atlassian.net",
            host: "gdncomm.atlassian.net",
            account: "someone@example.test",
            default: true,
            token_scoped: false,
            last_verified_at: "2026-09-01T10:00:00Z",
          },
          {
            id: "acme",
            adapter_id: "atlassian:acme",
            base_url: "https://acme.atlassian.net",
            host: "acme.atlassian.net",
            default: false,
            token_scoped: false,
          },
        ],
      },
      {
        id: "github",
        label: "GitHub",
        provider: "github",
        command: "node",
        entrypoint: "/opt/punakawan/adapters/github/dist/run.js",
        installed: true,
        organizations: [],
      },
    ],
    ...over,
  };
}

describe("ConnectorsPage", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse(connectors())));
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("shows each adapter with the organisations it holds credentials for", async () => {
    render(ConnectorsPage);

    await waitFor(() => expect(screen.getByText("Jira")).toBeTruthy());
    const jira = within(screen.getByLabelText("Jira organisations"));
    expect(jira.getByText("gdncomm")).toBeTruthy();
    expect(jira.getByText("acme")).toBeTruthy();
    // The default organisation is the one delivery work uses when it names
    // none, so it must be visibly distinguishable from the rest.
    expect(jira.getByText("default")).toBeTruthy();
  });

  it("tells you how to connect an adapter that has no account yet", async () => {
    render(ConnectorsPage);
    await waitFor(() => expect(screen.getByText("GitHub")).toBeTruthy());
    expect(screen.getByText("punakawan setup github")).toBeTruthy();
    expect(screen.getByText("No account")).toBeTruthy();
  });

  it("opens a detail dialog for one organisation", async () => {
    render(ConnectorsPage);
    await waitFor(() => expect(screen.getByText("gdncomm")).toBeTruthy());

    await fireEvent.click(screen.getByText("gdncomm"));

    const dialog = within(await screen.findByRole("dialog"));
    expect(dialog.getByText("atlassian:gdncomm")).toBeTruthy();
    expect(dialog.getByText("someone@example.test")).toBeTruthy();
  });

  it("says what to run when nothing is connected", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse(connectors({ adapters: [] }))));
    render(ConnectorsPage);
    await waitFor(() => expect(screen.getByText("punakawan setup jira")).toBeTruthy());
  });
});
