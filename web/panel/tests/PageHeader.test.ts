import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import PageHeaderHarness from "./fixtures/PageHeaderHarness.svelte";

describe("PageHeader", () => {
  it("renders the title", () => {
    render(PageHeaderHarness, { props: { title: "Overview" } });
    expect(screen.getByRole("heading", { name: "Overview" })).toBeTruthy();
  });

  it("renders an optional description", () => {
    render(PageHeaderHarness, { props: { title: "System", description: "Diagnostic facts." } });
    expect(screen.getByText("Diagnostic facts.")).toBeTruthy();
  });

  it("omits the description when none is given", () => {
    render(PageHeaderHarness, { props: { title: "System" } });
    expect(screen.queryByText("Diagnostic facts.")).toBeNull();
  });

  it("renders action slot content when provided", () => {
    render(PageHeaderHarness, { props: { title: "Tasks", withActions: true } });
    expect(screen.getByRole("button", { name: "Do thing" })).toBeTruthy();
  });
});
