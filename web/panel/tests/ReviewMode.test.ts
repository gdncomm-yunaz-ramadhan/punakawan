import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ReviewMode from "../src/routes/review/ReviewMode.svelte";
import * as reviewApi from "../src/lib/review/api";

// ActiveRevisionSummary (rendered once a review is no longer "draft")
// pulls in CommentResolutionChart, which constructs a real Chart.js
// instance - stub it out exactly like CommentResolutionChart.test.ts
// does, since jsdom has no canvas support.
vi.mock("chart.js", () => {
  class FakeChart {
    data: unknown;
    options: unknown;
    constructor(_canvas: unknown, config: { data: unknown; options: unknown }) {
      this.data = config.data;
      this.options = config.options;
    }
    update = vi.fn();
    destroy = vi.fn();
    static register = vi.fn();
  }
  return {
    Chart: FakeChart,
    BarController: class {},
    BarElement: class {},
    CategoryScale: class {},
    Legend: class {},
    LinearScale: class {},
    LineController: class {},
    LineElement: class {},
    LogarithmicScale: class {},
    PointElement: class {},
    Tooltip: class {},
  };
});

// ProposalReview (rendered once a proposal exists) pulls in
// VersionLineageGraphView, which constructs a real Cytoscape instance -
// stub it out exactly like VersionLineageGraphView.test.ts does, since
// jsdom has no canvas support.
vi.mock("cytoscape", () => {
  const factory = vi.fn(() => ({
    on: vi.fn(),
    destroy: vi.fn(),
    style: () => ({ update: vi.fn() }),
    zoom: vi.fn(() => 1),
    fit: vi.fn(),
    layout: () => ({ run: vi.fn() }),
  }));
  return { default: factory };
});

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

const queuedReview: reviewApi.ArtifactReview = {
  ...sampleReview,
  metadata: { ...sampleReview.metadata, status: "queued" },
};

const cancelledReview: reviewApi.ArtifactReview = {
  ...sampleReview,
  metadata: { ...sampleReview.metadata, status: "cancelled" },
};

const sampleRevisionRequest: reviewApi.ArtifactRevisionRequest = {
  metadata: {
    id: "revision-abc123",
    review_id: "review-1",
    submitted_at: "2026-07-23T01:00:00Z",
    submitted_by: "local",
  },
  base_artifact: { type: "plan", id: "plan-panel", version: 3, revision_hash: "sha256:abcdef1234567890" },
  workflow: { type: "revise_plan_from_review" },
  comments: { snapshot_hash: "sha256:deadbeef", count: 1 },
};

const sampleRun: reviewApi.RunReference = {
  run_id: "revision-abc123def456",
  parent_task_id: "revision-abc123def456",
};

const proposalReadyReview: reviewApi.ArtifactReview = {
  ...sampleReview,
  metadata: { ...sampleReview.metadata, status: "proposal_ready" },
};

const sampleProposal: reviewApi.ArtifactRevisionProposal = {
  metadata: {
    id: "proposal-1",
    review_id: "review-1",
    revision_request_id: "revreq-1",
    attempt: 1,
    status: "ready",
  },
  base: { artifact_id: "plan-panel", version: 3, revision_hash: "sha256:abcdef1234567890" },
  proposed: {
    version: 4,
    content_hash: "sha256:" + "a".repeat(64),
    content_location: ".punakawan/reviews/review-1/proposals/1.md",
  },
  results: {
    validation_status: "passed",
    comment_resolutions: [{ comment_id: "comment-1", status: "addressed", changed_block_ids: ["security-model"] }],
  },
};

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
  vi.spyOn(reviewApi, "submitReview").mockResolvedValue({ revision_request: sampleRevisionRequest, run: sampleRun });
  vi.spyOn(reviewApi, "cancelReview").mockResolvedValue(cancelledReview);
  vi.spyOn(reviewApi, "getTimeline").mockResolvedValue({
    review: queuedReview,
    comment_count: sampleComments.length,
    revision_request: sampleRevisionRequest,
    run: sampleRun,
  });
  vi.spyOn(reviewApi, "listProposals").mockResolvedValue({ items: [sampleProposal] });
  vi.spyOn(reviewApi, "getProposalDiff").mockResolvedValue({
    lines: [{ Kind: "equal", Text: "# Plan Panel" }],
    summary: { added: 0, removed: 0 },
  });
  vi.spyOn(reviewApi, "getProposalValidation").mockResolvedValue({
    structural: { passed: true, issues: [] },
    compliance: { passed: true, issues: [] },
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
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

  // Responsive "visual regression" per apy.7's hardening pass (§21 "usable
  // at 360px, 768px, 1024px, and wide desktop widths"): jsdom has no
  // layout engine, so this asserts DOM structure via the forceWidth seam
  // at each of the plan's four named breakpoints, rather than pixel
  // screenshots (not feasible in this environment).
  describe("breakpoint matrix (360 / 768 / 1024 / wide)", () => {
    it.each([
      { label: "360px", width: 360 },
      { label: "768px", width: 768 },
    ])("renders the mobile document + bottom-sheet comment rail at $label", async ({ width }) => {
      render(ReviewMode, { props: { reviewId: "review-1", forceWidth: width } });

      await waitFor(() => expect(screen.getByTestId("view-comments-toggle")).toBeTruthy());
      expect(screen.getByTestId("review-mode").getAttribute("data-layout")).toBe("mobile");
      expect(screen.getByTestId("plan-document")).toBeTruthy();
      expect(screen.queryByTestId("comment-rail")).toBeNull();

      await fireEvent.click(screen.getByTestId("view-comments-toggle"));
      expect(within(screen.getByRole("dialog")).getByTestId("comment-rail")).toBeTruthy();
    });

    it.each([
      { label: "1024px", width: 1024 },
      { label: "1440px+ (wide)", width: 1600 },
    ])("renders the desktop two-pane document + comment rail at $label", async ({ width }) => {
      render(ReviewMode, { props: { reviewId: "review-1", forceWidth: width } });

      await waitFor(() => expect(screen.getByTestId("plan-document")).toBeTruthy());
      expect(screen.getByTestId("review-mode").getAttribute("data-layout")).toBe("desktop");
      expect(screen.getByTestId("comment-rail")).toBeTruthy();
      expect(screen.queryByTestId("view-comments-toggle")).toBeNull();
    });
  });

  it("shows an enabled Submit Review button for a draft review", async () => {
    render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });

    await waitFor(() => expect(screen.getByText("Submit Review")).toBeTruthy());
    const button = screen.getByText("Submit Review") as HTMLButtonElement;
    expect(button.disabled).toBe(false);
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

  describe("submit", () => {
    it("calls the submit endpoint and transitions to the active-revision view on success", async () => {
      render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });

      await waitFor(() => expect(screen.getByText("Submit Review")).toBeTruthy());
      await fireEvent.click(screen.getByText("Submit Review"));

      await waitFor(() => expect(reviewApi.submitReview).toHaveBeenCalledWith("review-1"));
      await waitFor(() => expect(screen.getByTestId("active-revision-summary")).toBeTruthy());
      // Editable review-mode UI (document + comment rail) is gone.
      expect(screen.queryByTestId("plan-document")).toBeNull();
      // Tracked run id from the submit response surfaces plainly.
      expect(screen.getByText(/revision-abc123def456/)).toBeTruthy();
    });

    it("disables the button while the request is in flight to avoid a double submit", async () => {
      let resolveSubmit: (v: reviewApi.SubmitReviewResponse) => void = () => {};
      (reviewApi.submitReview as ReturnType<typeof vi.fn>).mockReturnValue(
        new Promise((resolve) => {
          resolveSubmit = resolve;
        }),
      );

      render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });
      await waitFor(() => expect(screen.getByText("Submit Review")).toBeTruthy());

      const button = screen.getByText("Submit Review") as HTMLButtonElement;
      await fireEvent.click(button);
      await fireEvent.click(button);
      await fireEvent.click(button);

      expect(reviewApi.submitReview).toHaveBeenCalledTimes(1);
      resolveSubmit({ revision_request: sampleRevisionRequest, run: sampleRun });
    });

    it("shows a specific error message on 409 and does not transition to the active-revision view", async () => {
      (reviewApi.submitReview as ReturnType<typeof vi.fn>).mockRejectedValue(
        new reviewApi.ApiError(409, "api: review review-1 is not a draft and has no matching pending submission"),
      );

      render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });
      await waitFor(() => expect(screen.getByText("Submit Review")).toBeTruthy());
      await fireEvent.click(screen.getByText("Submit Review"));

      await waitFor(() => {
        expect(screen.getByText(/not a draft and has no matching pending submission/)).toBeTruthy();
      });
      expect(screen.queryByTestId("active-revision-summary")).toBeNull();
      expect(screen.getByTestId("plan-document")).toBeTruthy();
    });
  });

  describe("cancel", () => {
    it("requires confirmation before firing the cancel request", async () => {
      (reviewApi.getReview as ReturnType<typeof vi.fn>).mockResolvedValue(queuedReview);

      render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });
      await waitFor(() => expect(screen.getByTestId("active-revision-summary")).toBeTruthy());

      await fireEvent.click(screen.getByTestId("cancel-review-button"));
      expect(screen.getByRole("dialog")).toBeTruthy();
      expect(reviewApi.cancelReview).not.toHaveBeenCalled();

      await fireEvent.click(screen.getByText("Keep Review"));
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(reviewApi.cancelReview).not.toHaveBeenCalled();
    });

    it("calls the cancel endpoint once the user confirms", async () => {
      (reviewApi.getReview as ReturnType<typeof vi.fn>).mockResolvedValue(queuedReview);

      render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });
      await waitFor(() => expect(screen.getByTestId("active-revision-summary")).toBeTruthy());

      await fireEvent.click(screen.getByTestId("cancel-review-button"));
      const dialog = screen.getByRole("dialog");
      await fireEvent.click(within(dialog).getByText("Cancel Review"));

      await waitFor(() => expect(reviewApi.cancelReview).toHaveBeenCalledWith("review-1"));
    });
  });

  describe("timeline polling", () => {
    beforeEach(() => {
      vi.useFakeTimers({ shouldAdvanceTime: true });
    });

    it("polls the timeline endpoint again after the interval elapses", async () => {
      (reviewApi.getReview as ReturnType<typeof vi.fn>).mockResolvedValue(queuedReview);

      render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });
      await vi.waitFor(() => expect(screen.getByTestId("active-revision-summary")).toBeTruthy());

      const callsBefore = (reviewApi.getTimeline as ReturnType<typeof vi.fn>).mock.calls.length;
      await vi.advanceTimersByTimeAsync(8000);
      expect((reviewApi.getTimeline as ReturnType<typeof vi.fn>).mock.calls.length).toBeGreaterThan(callsBefore);
    });

    it("stops polling once a terminal status is observed", async () => {
      (reviewApi.getReview as ReturnType<typeof vi.fn>).mockResolvedValue(queuedReview);
      (reviewApi.getTimeline as ReturnType<typeof vi.fn>).mockResolvedValue({
        review: { ...queuedReview, metadata: { ...queuedReview.metadata, status: "accepted" } },
        comment_count: sampleComments.length,
        revision_request: sampleRevisionRequest,
        run: sampleRun,
      });

      render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });
      await vi.waitFor(() => expect(screen.getByTestId("active-revision-summary")).toBeTruthy());

      await vi.advanceTimersByTimeAsync(8000);
      const callsAfterFirstPoll = (reviewApi.getTimeline as ReturnType<typeof vi.fn>).mock.calls.length;
      expect(callsAfterFirstPoll).toBeGreaterThan(0);

      await vi.advanceTimersByTimeAsync(24000);
      expect((reviewApi.getTimeline as ReturnType<typeof vi.fn>).mock.calls.length).toBe(callsAfterFirstPoll);
    });
  });

  describe("clarification", () => {
    it("surfaces a needs_clarification comment and answers it via updateComment", async () => {
      const clarificationComment: reviewApi.ArtifactComment = {
        ...sampleComments[0],
        id: "comment-2",
        status: "needs_clarification",
        body: "Which environment should this target?",
      };
      (reviewApi.getReview as ReturnType<typeof vi.fn>).mockResolvedValue(queuedReview);
      (reviewApi.listComments as ReturnType<typeof vi.fn>).mockResolvedValue({
        items: [...sampleComments, clarificationComment],
      });
      (reviewApi.updateComment as ReturnType<typeof vi.fn>).mockResolvedValue({
        ...clarificationComment,
        status: "resolved_by_user",
        body: "Staging.",
      });

      render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });
      await waitFor(() => expect(screen.getByTestId("clarification-section")).toBeTruthy());
      expect(screen.getByText("Which environment should this target?")).toBeTruthy();

      await fireEvent.input(screen.getByTestId("clarification-input-comment-2"), {
        target: { value: "Staging." },
      });
      await fireEvent.click(screen.getByTestId("clarification-save-comment-2"));

      await waitFor(() =>
        expect(reviewApi.updateComment).toHaveBeenCalledWith("review-1", "comment-2", {
          body: "Staging.",
          status: "resolved_by_user",
        }),
      );
    });
  });

  describe("proposal review", () => {
    it("renders ProposalReview instead of ActiveRevisionSummary once a proposal exists", async () => {
      (reviewApi.getReview as ReturnType<typeof vi.fn>).mockResolvedValue(proposalReadyReview);

      render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });

      await waitFor(() => expect(screen.getByTestId("proposal-review")).toBeTruthy());
      expect(screen.queryByTestId("active-revision-summary")).toBeNull();
      expect(reviewApi.listProposals).toHaveBeenCalledWith("review-1");
    });

    it("still renders ActiveRevisionSummary (not ProposalReview) for in-flight statuses with no proposal yet", async () => {
      (reviewApi.getReview as ReturnType<typeof vi.fn>).mockResolvedValue(queuedReview);

      render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });

      await waitFor(() => expect(screen.getByTestId("active-revision-summary")).toBeTruthy());
      expect(screen.queryByTestId("proposal-review")).toBeNull();
    });

    it("refetches comments and re-renders after an accept action changes the review", async () => {
      (reviewApi.getReview as ReturnType<typeof vi.fn>).mockResolvedValue(proposalReadyReview);
      const acceptedReview: reviewApi.ArtifactReview = {
        ...proposalReadyReview,
        metadata: { ...proposalReadyReview.metadata, status: "accepted" },
      };
      vi.spyOn(reviewApi, "acceptProposal").mockResolvedValue({ review: acceptedReview, new_version: {} });

      render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });
      await waitFor(() => expect(screen.getByTestId("accept-button")).toBeTruthy());

      await fireEvent.click(screen.getByTestId("accept-button"));
      const dialog = screen.getByRole("dialog");
      await fireEvent.click(within(dialog).getByText("Accept Proposal"));

      await waitFor(() => expect(reviewApi.acceptProposal).toHaveBeenCalledWith("review-1", "1"));
      await waitFor(() => expect(screen.queryByTestId("accept-button")).toBeNull());
    });
  });

  describe("retrieval_recipe review", () => {
    const recipeReview: reviewApi.ArtifactReview = {
      metadata: {
        id: "review-2",
        workspace_id: "ws-1",
        status: "draft",
        created_by: "local",
        created_at: "2026-07-23T00:00:00Z",
      },
      artifact: {
        type: "retrieval_recipe",
        id: "pkw:recipe/affiliate-api/jira-next-sprint",
        version: 1,
        revision_hash: "sha256:0123456789abcdef",
      },
      review: { title: "Tighten the project filter" },
    };
    const recipeContent: reviewApi.ArtifactContent = {
      content: JSON.stringify(
        { id: "pkw:recipe/affiliate-api/jira-next-sprint", retrieval_recipe: { capability: "jira.issue.search" } },
        null,
        2,
      ),
      reference: {
        type: "retrieval_recipe",
        id: "pkw:recipe/affiliate-api/jira-next-sprint",
        version: 1,
        revision_hash: "sha256:0123456789abcdef",
        workspace_id: "ws-1",
        format: "json",
      },
    };

    it("renders RecipeDocument (not PlanDocument) and comments with a recipe_field_path anchor", async () => {
      (reviewApi.getReview as ReturnType<typeof vi.fn>).mockResolvedValue(recipeReview);
      (reviewApi.getArtifactCurrent as ReturnType<typeof vi.fn>).mockResolvedValue(recipeContent);
      (reviewApi.listComments as ReturnType<typeof vi.fn>).mockResolvedValue({ items: [] });

      render(ReviewMode, { props: { reviewId: "review-2", forceWidth: 1280 } });

      await waitFor(() => expect(screen.getByTestId("recipe-document")).toBeTruthy());
      expect(screen.queryByTestId("plan-document")).toBeNull();

      const capabilityNode = screen
        .getAllByTestId("recipe-field-node")
        .find((n) => n.getAttribute("data-field-path") === "retrieval_recipe.capability");
      expect(capabilityNode).toBeTruthy();
      await fireEvent.click(capabilityNode!.querySelector('[data-testid="comment-field-button"]')!);

      const popover = await screen.findByTestId("add-comment-popover");
      expect(within(popover).getByText("retrieval_recipe.capability")).toBeTruthy();

      await fireEvent.input(screen.getByTestId("comment-body-input"), {
        target: { value: "This should be AFFILIATE, not AFF." },
      });
      await fireEvent.click(screen.getByRole("button", { name: "Add Comment" }));

      await waitFor(() => expect(reviewApi.createComment).toHaveBeenCalled());
      const [, req] = (reviewApi.createComment as ReturnType<typeof vi.fn>).mock.calls[0];
      expect(req.anchor).toEqual({
        kind: "recipe_field_path",
        base_revision_hash: "sha256:0123456789abcdef",
        field_path: "retrieval_recipe.capability",
      });
    });
  });
});
