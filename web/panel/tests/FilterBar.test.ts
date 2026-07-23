import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it, vi } from "vitest";
import FilterBarHarness from "./fixtures/FilterBarHarness.svelte";

describe("FilterBar", () => {
  it("renders without crashing with a search input", () => {
    render(FilterBarHarness);
    expect(screen.getByPlaceholderText("Search reviews")).toBeTruthy();
  });

  it("updates its bound value as the user types", async () => {
    render(FilterBarHarness);
    const input = screen.getByPlaceholderText("Search reviews");
    await fireEvent.input(input, { target: { value: "checkout" } });
    expect(screen.getByTestId("filter-bar-value").textContent).toBe("checkout");
  });

  it("calls onSearchChange as the user types", async () => {
    const onSearchChange = vi.fn();
    render(FilterBarHarness, { props: { onSearchChange } });
    const input = screen.getByPlaceholderText("Search reviews");
    await fireEvent.input(input, { target: { value: "abc" } });
    expect(onSearchChange).toHaveBeenCalledWith("abc");
  });

  it("renders additional filter controls via the filters slot", () => {
    render(FilterBarHarness, { props: { withFilters: true } });
    expect(screen.getByTestId("status-filter")).toBeTruthy();
  });
});
