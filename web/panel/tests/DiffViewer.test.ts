import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import DiffViewer from "../src/lib/components/review/DiffViewer.svelte";
import type { DiffLine } from "../src/lib/review/api";

function equalRun(count: number, prefix = "line"): DiffLine[] {
  return Array.from({ length: count }, (_, i) => ({ Kind: "equal" as const, Text: `${prefix} ${i}` }));
}

describe("DiffViewer", () => {
  it("renders added/removed/equal lines in unified mode with +/- prefixes", () => {
    const lines: DiffLine[] = [
      { Kind: "equal", Text: "unchanged line" },
      { Kind: "removed", Text: "old line" },
      { Kind: "added", Text: "new line" },
    ];
    render(DiffViewer, { props: { lines, isDesktop: false } });

    expect(screen.getByTestId("diff-unified")).toBeTruthy();
    expect(screen.getByText("unchanged line")).toBeTruthy();
    expect(screen.getByText("old line")).toBeTruthy();
    expect(screen.getByText("new line")).toBeTruthy();
  });

  it("renders side-by-side panes on desktop", () => {
    const lines: DiffLine[] = [
      { Kind: "removed", Text: "old line" },
      { Kind: "added", Text: "new line" },
    ];
    render(DiffViewer, { props: { lines, isDesktop: true } });

    expect(screen.getByTestId("diff-side-by-side")).toBeTruthy();
    expect(screen.queryByTestId("diff-unified")).toBeNull();
  });

  it("does not collapse a short run of equal lines (<=6)", () => {
    const lines = equalRun(6);
    render(DiffViewer, { props: { lines, isDesktop: false } });

    expect(screen.queryByTestId("diff-collapsed-toggle")).toBeNull();
    for (let i = 0; i < 6; i++) {
      expect(screen.getByText(`line ${i}`)).toBeTruthy();
    }
  });

  it("collapses a long run of consecutive equal lines behind a toggle", () => {
    const lines = equalRun(10);
    render(DiffViewer, { props: { lines, isDesktop: false } });

    const toggle = screen.getByTestId("diff-collapsed-toggle");
    expect(toggle.textContent).toContain("Show 10 unchanged lines");
    expect(screen.queryByText("line 0")).toBeNull();
  });

  it("expands a collapsed run when the toggle is clicked", async () => {
    const lines = equalRun(10);
    render(DiffViewer, { props: { lines, isDesktop: false } });

    await fireEvent.click(screen.getByTestId("diff-collapsed-toggle"));

    expect(screen.getByText("line 0")).toBeTruthy();
    expect(screen.getByText("line 9")).toBeTruthy();
    expect(screen.getByTestId("diff-collapsed-toggle").textContent).toContain("Hide");
  });

  it("filters visible lines by the search term", async () => {
    const lines: DiffLine[] = [
      { Kind: "equal", Text: "alpha context" },
      { Kind: "removed", Text: "beta removed" },
      { Kind: "added", Text: "gamma added" },
    ];
    render(DiffViewer, { props: { lines, isDesktop: false } });

    await fireEvent.input(screen.getByTestId("diff-search"), { target: { value: "gamma" } });

    expect(screen.queryByText("alpha context")).toBeNull();
    expect(screen.queryByText("beta removed")).toBeNull();
    expect(screen.getByText("gamma")).toBeTruthy();
  });

  it("highlights the matching substring within a line", async () => {
    const lines: DiffLine[] = [{ Kind: "added", Text: "the quick fox" }];
    render(DiffViewer, { props: { lines, isDesktop: false } });

    await fireEvent.input(screen.getByTestId("diff-search"), { target: { value: "quick" } });

    const mark = document.querySelector("mark");
    expect(mark?.textContent).toBe("quick");
  });
});
