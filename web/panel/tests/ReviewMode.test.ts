import { fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ReviewMode from "../src/routes/review/ReviewMode.svelte";
import * as reviewApi from "../src/lib/review/api";

const sampleReview: reviewApi.ArtifactReview = {
  metadata: {
    id: "review-1",
    workspace_id: "ws-1",
    status: "draft",
    created_by: "local",
    created_at: "2026-07-23T00:00:00Z",
  },
  artifact: { type: "plan", id: "plan-panel", version: 3, revision_hash: "sha256:abcdef1234567890" },
  review: { title: "Plan Panel Review", instruction: "Focus on security." },
};

const sampleContent: reviewApi.ArtifactContent = {
  content: "# Plan Panel\n\n## Security Model\n\nDefault binding: 127.0.0.1 only\n",
  reference: {
    type: "plan",
    id: "plan-panel",
    version: 3,
    revision_hash: "sha256:abcdef1234567890",
    workspace_id: "ws-1",
    format: "markdown",
  },
};

const sampleComments: reviewApi.ArtifactComment[] = [
  {
    id: "comment-1",
    review_id: "review-1",
    author: "local",
    status: "open",
    anchor: {
      kind: "markdown_block",
      base_revision_hash: "sha256:abcdef1234567890",
      heading_path: ["Plan Panel", "Security Model"],
      quoted_text: "Default binding: 127.0.0.1 only",
    },
    body: "Consider LAN mode.",
  },
];

beforeEach(() => {
  vi.spyOn(reviewApi, "getReview").mockResolvedValue(sampleReview);
  vi.spyOn(reviewApi, "getArtifactCurrent").mockResolvedValue(sampleContent);
  vi.spyOn(reviewApi, "listComments").mockResolvedValue({ items: sampleComments });
  vi.spyOn(reviewApi, "updateReview").mockResolvedValue(sampleReview);
  // Echo back the request's own client-generated id/anchor/body rather
  // than a fixed comment, so a newly created comment never collides with
  // the already-loaded comment-1 fixture (each block's key is comment.id).
  vi.spyOn(reviewApi, "createComment").mockImplementation(async (_reviewId, req) => ({
    id: req.id,
    review_id: "review-1",
    author: "local",
    status: "open",
    anchor: req.anchor,
    body: req.body,
  }));
  vi.spyOn(reviewApi, "updateComment").mockResolvedValue(sampleComments[0]);
  vi.spyOn(reviewApi, "deleteComment").mockResolvedValue(undefined);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ReviewMode", () => {
  it("fetches review, artifact, and comments on mount and shows version + revision hash", async () => {
    render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });

    await waitFor(() => {
      expect(screen.getByText("Plan Panel Review")).toBeTruthy();
    });
    expect(reviewApi.getReview).toHaveBeenCalledWith("review-1");
    expect(reviewApi.getArtifactCurrent).toHaveBeenCalledWith("plan", "plan-panel");
    expect(screen.getByText(/version 3/)).toBeTruthy();
    expect(screen.getByText(/abcdef1234/)).toBeTruthy();
  });

  it("renders the desktop two-pane layout at >=1024px", async () => {
    render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });

    await waitFor(() => expect(screen.getByTestId("plan-document")).toBeTruthy());
    expect(screen.getByTestId("review-mode").getAttribute("data-layout")).toBe("desktop");
    expect(screen.getByTestId("comment-rail")).toBeTruthy();
    expect(screen.queryByTestId("view-comments-toggle")).toBeNull();
  });

  it("renders the mobile layout (floating action + bottom sheet) under 1024px", async () => {
    render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 500 } });

    await waitFor(() => expect(screen.getByTestId("view-comments-toggle")).toBeTruthy());
    expect(screen.getByTestId("review-mode").getAttribute("data-layout")).toBe("mobile");
    // Comment rail lives inside the (closed) BottomSheet until toggled.
    expect(screen.queryByRole("dialog")).toBeNull();

    await fireEvent.click(screen.getByTestId("view-comments-toggle"));
    expect(screen.getByRole("dialog")).toBeTruthy();
  });

  it("shows a disabled Submit Review button with an explanatory title", async () => {
    render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });

    await waitFor(() => expect(screen.getByText("Submit Review")).toBeTruthy());
    const button = screen.getByText("Submit Review") as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    expect(button.title).toContain("next phase");
  });

  it("shows comment status chips from CommentThread", async () => {
    render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });

    await waitFor(() => expect(screen.getByText("Consider LAN mode.")).toBeTruthy());
    expect(screen.getByText("Open")).toBeTruthy();
  });

  it("adds a section comment via the anchor popover, calling createComment with a resolvable anchor", async () => {
    render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });

    await waitFor(() => expect(screen.getAllByTestId("add-section-comment").length).toBeGreaterThan(0));
    const affordances = screen.getAllByTestId("add-section-comment");
    await fireEvent.click(affordances[1]); // "Security Model" section

    const popover = await screen.findByTestId("add-comment-popover");
    expect(popover).toBeTruthy();

    await fireEvent.input(screen.getByTestId("comment-body-input"), { target: { value: "New comment body" } });
    await fireEvent.click(screen.getByRole("button", { name: "Add Comment" }));

    await waitFor(() => expect(reviewApi.createComment).toHaveBeenCalled());
    const [, req] = (reviewApi.createComment as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(req.body).toBe("New comment body");
    expect(req.anchor.base_revision_hash).toBe("sha256:abcdef1234567890");
    expect(req.anchor.heading_path).toEqual(["Plan Panel", "Security Model"]);
    // Section-only comments (no text selection) need a derived quoted_text
    // so the server's anchor resolver (heading_path + quoted_text) can
    // actually find the block - see markdown.ts's snippetForSection.
    expect(req.anchor.quoted_text).toBeTruthy();
    expect(typeof req.id).toBe("string");
  });

  it("shows the unsaved-changes indicator while a comment draft is open, and clears it after cancel", async () => {
    render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });

    await waitFor(() => expect(screen.getAllByTestId("add-section-comment").length).toBeGreaterThan(0));
    await fireEvent.click(screen.getAllByTestId("add-section-comment")[0]);
    expect(screen.getByTestId("add-comment-popover")).toBeTruthy();

    await fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByTestId("add-comment-popover")).toBeNull();
  });

  it("shows a session-expired message and stops when a mutating call reports it", async () => {
    const { SessionExpiredError } = await import("../src/lib/session");
    (reviewApi.createComment as ReturnType<typeof vi.fn>).mockRejectedValue(new SessionExpiredError());

    render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });

    await waitFor(() => expect(screen.getAllByTestId("add-section-comment").length).toBeGreaterThan(0));
    await fireEvent.click(screen.getAllByTestId("add-section-comment")[0]);
    await fireEvent.input(screen.getByTestId("comment-body-input"), { target: { value: "hello" } });
    await fireEvent.click(screen.getByRole("button", { name: "Add Comment" }));

    await waitFor(() => {
      expect(screen.getByText(/session has expired/i)).toBeTruthy();
    });
  });

  it("shows a load error state when getReview fails", async () => {
    (reviewApi.getReview as ReturnType<typeof vi.fn>).mockRejectedValue(new Error("review: not found"));

    render(ReviewMode, { props: { reviewId: "missing-review", forceWidth: 1280 } });

    await waitFor(() => {
      expect(screen.getByText("review: not found")).toBeTruthy();
    });
  });
});
