import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import DiffSummaryCard from "../src/lib/components/cards/DiffSummaryCard.svelte";

describe("DiffSummaryCard", () => {
  it("renders without crashing and shows added/removed/modified counts", () => {
    render(DiffSummaryCard, { props: { added: 12, removed: 4, modified: 7 } });
    expect(screen.getByText("12")).toBeTruthy();
    expect(screen.getByText("4")).toBeTruthy();
    expect(screen.getByText("7")).toBeTruthy();
    expect(screen.getByText("Added")).toBeTruthy();
    expect(screen.getByText("Removed")).toBeTruthy();
    expect(screen.getByText("Modified")).toBeTruthy();
  });

  it("applies distinct semantic classes to each metric", () => {
    render(DiffSummaryCard, { props: { added: 1, removed: 2, modified: 3 } });
    expect(document.querySelector(".metric-added")).toBeTruthy();
    expect(document.querySelector(".metric-removed")).toBeTruthy();
    expect(document.querySelector(".metric-modified")).toBeTruthy();
  });
});
