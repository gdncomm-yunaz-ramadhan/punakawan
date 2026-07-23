import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it, vi } from "vitest";
import CommentThread from "../src/lib/components/review/CommentThread.svelte";
import type { ArtifactComment } from "../src/lib/review/api";

function comment(overrides: Partial<ArtifactComment> = {}): ArtifactComment {
  return {
    id: "comment-1",
    review_id: "review-1",
    author: "local",
    status: "open",
    anchor: { kind: "markdown_block", base_revision_hash: "sha256:x", heading_path: ["A", "B"], quoted_text: "quote" },
    body: "a comment",
    ...overrides,
  };
}

describe("CommentThread", () => {
  it("renders the heading path, quoted text, body, and author", () => {
    render(CommentThread, { props: { comment: comment(), editable: true, onedit: () => {}, ondelete: () => {} } });

    expect(screen.getByText("A › B")).toBeTruthy();
    expect(screen.getByText(/quote/)).toBeTruthy();
    expect(screen.getByText("a comment")).toBeTruthy();
    expect(screen.getByText("local")).toBeTruthy();
  });

  it.each([
    ["open", "Open"],
    ["addressed", "Addressed"],
    ["partially_addressed", "Partially addressed"],
    ["rejected_by_agent", "Rejected by agent"],
    ["needs_clarification", "Needs clarification"],
    ["resolved_by_user", "Resolved"],
    ["obsolete", "Deleted"],
  ])("renders a status chip for %s", (status, label) => {
    render(CommentThread, {
      props: {
        comment: comment({ status: status as ArtifactComment["status"] }),
        editable: true,
        onedit: () => {},
        ondelete: () => {},
      },
    });
    expect(screen.getByText(label)).toBeTruthy();
  });

  it("hides edit/delete actions when not editable", () => {
    render(CommentThread, { props: { comment: comment(), editable: false, onedit: () => {}, ondelete: () => {} } });
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete" })).toBeNull();
  });

  it("hides edit/delete actions for an obsolete comment even if editable=true", () => {
    render(CommentThread, {
      props: { comment: comment({ status: "obsolete" }), editable: true, onedit: () => {}, ondelete: () => {} },
    });
    expect(screen.queryByRole("button", { name: "Edit" })).toBeNull();
  });

  it("switches to edit mode and calls onedit with the trimmed body", async () => {
    const onedit = vi.fn();
    render(CommentThread, { props: { comment: comment(), editable: true, onedit, ondelete: () => {} } });

    await fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    const textarea = screen.getByTestId("comment-edit-input");
    await fireEvent.input(textarea, { target: { value: "  updated body  " } });
    await fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(onedit).toHaveBeenCalledWith("updated body");
  });

  it("calls ondelete when Delete is clicked", async () => {
    const ondelete = vi.fn();
    render(CommentThread, { props: { comment: comment(), editable: true, onedit: () => {}, ondelete } });

    await fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(ondelete).toHaveBeenCalled();
  });

  it("disables actions while busy", () => {
    render(CommentThread, {
      props: { comment: comment(), editable: true, busy: true, onedit: () => {}, ondelete: () => {} },
    });
    expect((screen.getByRole("button", { name: "Edit" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Delete" }) as HTMLButtonElement).disabled).toBe(true);
  });
});
