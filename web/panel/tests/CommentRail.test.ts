import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import CommentRail from "../src/lib/components/review/CommentRail.svelte";
import type { ArtifactComment } from "../src/lib/review/api";

function comment(id: string, headingPath: string[], overrides: Partial<ArtifactComment> = {}): ArtifactComment {
  return {
    id,
    review_id: "review-1",
    author: "local",
    status: "open",
    anchor: { kind: "markdown_block", base_revision_hash: "sha256:x", heading_path: headingPath },
    body: `body for ${id}`,
    ...overrides,
  };
}

describe("CommentRail", () => {
  it("shows the empty state when there are no comments", () => {
    render(CommentRail, {
      props: {
        comments: [],
        documentHeadingOrder: [],
        editable: true,
        onEditComment: () => {},
        onDeleteComment: () => {},
      },
    });
    expect(screen.getByText(/No comments yet/)).toBeTruthy();
  });

  it("orders comments by their heading's position in the document, not creation order", () => {
    const comments = [
      comment("c-late", ["Doc › Third"]),
      comment("c-early", ["Doc › First"]),
      comment("c-mid", ["Doc › Second"]),
    ];
    render(CommentRail, {
      props: {
        comments,
        documentHeadingOrder: ["Doc › First", "Doc › Second", "Doc › Third"],
        editable: true,
        onEditComment: () => {},
        onDeleteComment: () => {},
      },
    });

    const bodies = screen.getAllByText(/^body for /).map((el) => el.textContent);
    expect(bodies).toEqual(["body for c-early", "body for c-mid", "body for c-late"]);
  });

  it("filters out obsolete comments when showObsolete is false", () => {
    const comments = [comment("c-1", ["A"]), comment("c-2", ["A"], { status: "obsolete" })];
    render(CommentRail, {
      props: {
        comments,
        documentHeadingOrder: ["A"],
        editable: true,
        showObsolete: false,
        onEditComment: () => {},
        onDeleteComment: () => {},
      },
    });

    expect(screen.getByText("body for c-1")).toBeTruthy();
    expect(screen.queryByText("body for c-2")).toBeNull();
  });
});
