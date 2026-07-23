import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it, vi } from "vitest";
import PlanDocument from "../src/lib/components/review/PlanDocument.svelte";

const sampleMarkdown = `# Punakawan Panel

## Security Model

Default binding: 127.0.0.1 only

## Rollout

Ships behind a feature flag.
`;

describe("PlanDocument", () => {
  it("renders headings and paragraphs, preserving pk:block markers as invisible", () => {
    const markdownWithMarker = `# Title\n\n<!-- pk:block:my.block -->\n## Section\n\nBody text.\n`;
    render(PlanDocument, {
      props: { content: markdownWithMarker, onCommentSection: () => {}, onCommentSelection: () => {} },
    });

    expect(screen.getByRole("heading", { level: 1, name: "Title" })).toBeTruthy();
    expect(screen.getByRole("heading", { level: 2, name: "Section" })).toBeTruthy();
    expect(screen.getByText("Body text.")).toBeTruthy();
    expect(screen.getByTestId("plan-document").textContent).not.toContain("pk:block");
    expect(screen.getByTestId("plan-document").textContent).not.toContain("<!--");
  });

  it("clicking a section's add-comment affordance calls onCommentSection with heading_path only", async () => {
    const onCommentSection = vi.fn();
    render(PlanDocument, {
      props: { content: sampleMarkdown, onCommentSection, onCommentSelection: () => {} },
    });

    const affordances = screen.getAllByTestId("add-section-comment");
    // Second heading is "Security Model".
    await fireEvent.click(affordances[1]);

    expect(onCommentSection).toHaveBeenCalledWith(
      expect.objectContaining({
        headingPath: ["Punakawan Panel", "Security Model"],
        quotedText: "Default binding: 127.0.0.1 only",
      }),
    );
  });

  it("shows a selection popover only when a non-empty selection exists inside the document", async () => {
    render(PlanDocument, {
      props: { content: sampleMarkdown, onCommentSection: () => {}, onCommentSelection: () => {} },
    });

    expect(screen.queryByTestId("selection-popover")).toBeNull();

    const paragraph = screen.getByText("Ships behind a feature flag.");
    const range = document.createRange();
    range.selectNodeContents(paragraph);
    const sel = window.getSelection();
    sel?.removeAllRanges();
    sel?.addRange(range);
    document.dispatchEvent(new Event("selectionchange"));

    expect(await screen.findByTestId("selection-popover")).toBeTruthy();
  });

  it("clicking Comment on selection calls onCommentSelection with heading_path + quoted_text and clears the popover", async () => {
    const onCommentSelection = vi.fn();
    render(PlanDocument, {
      props: { content: sampleMarkdown, onCommentSection: () => {}, onCommentSelection },
    });

    const paragraph = screen.getByText("Ships behind a feature flag.");
    const range = document.createRange();
    range.selectNodeContents(paragraph);
    const sel = window.getSelection();
    sel?.removeAllRanges();
    sel?.addRange(range);
    document.dispatchEvent(new Event("selectionchange"));

    const popoverButton = await screen.findByText("Comment on selection");
    await fireEvent.click(popoverButton);

    expect(onCommentSelection).toHaveBeenCalledWith(
      expect.objectContaining({
        headingPath: ["Punakawan Panel", "Rollout"],
        quotedText: "Ships behind a feature flag.",
      }),
    );
    expect(screen.queryByTestId("selection-popover")).toBeNull();
  });

  it("hides the selection popover when the selection is collapsed (empty)", async () => {
    render(PlanDocument, {
      props: { content: sampleMarkdown, onCommentSection: () => {}, onCommentSelection: () => {} },
    });

    const sel = window.getSelection();
    sel?.removeAllRanges();
    document.dispatchEvent(new Event("selectionchange"));

    expect(screen.queryByTestId("selection-popover")).toBeNull();
  });
});
