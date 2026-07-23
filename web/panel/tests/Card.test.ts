import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import CardHarness from "./fixtures/CardHarness.svelte";

describe("Card", () => {
  it("renders without crashing and shows default content", () => {
    render(CardHarness, { props: { state: "default" } });
    expect(screen.getByTestId("card-content")).toBeTruthy();
  });

  it("swaps in a loading skeleton instead of content when state is 'loading'", () => {
    render(CardHarness, { props: { state: "loading" } });
    expect(screen.getByTestId("card-skeleton")).toBeTruthy();
    expect(screen.queryByTestId("card-content")).toBeNull();
  });

  it("swaps in an empty message instead of content when state is 'empty'", () => {
    render(CardHarness, { props: { state: "empty", emptyMessage: "Nothing to see." } });
    expect(screen.getByTestId("card-empty").textContent).toContain("Nothing to see.");
    expect(screen.queryByTestId("card-content")).toBeNull();
  });

  it("swaps in a warning banner instead of content when state is 'warning'", () => {
    render(CardHarness, { props: { state: "warning", warningMessage: "Careful." } });
    expect(screen.getByTestId("card-warning").textContent).toContain("Careful.");
    expect(screen.queryByTestId("card-content")).toBeNull();
  });

  it("swaps in an error banner instead of content when state is 'error'", () => {
    render(CardHarness, { props: { state: "error", errorMessage: "Broke." } });
    expect(screen.getByRole("alert").textContent).toContain("Broke.");
    expect(screen.queryByTestId("card-content")).toBeNull();
  });
});
