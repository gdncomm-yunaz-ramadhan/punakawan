import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ProposalReview from "../src/lib/components/review/ProposalReview.svelte";
import * as reviewApi from "../src/lib/review/api";
import type { ArtifactComment, ArtifactReview, ArtifactRevisionProposal } from "../src/lib/review/api";

// VersionLineageGraphView pulls in a real Cytoscape instance - stub it out
// exactly like VersionLineageGraphView.test.ts/RelationGraphView.test.ts do,
// since jsdom has no canvas support.
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

function review(overrides: Partial<ArtifactReview["metadata"]> = {}): ArtifactReview {
  return {
    metadata: {
      id: "review-1",
      workspace_id: "ws-1",
      status: "proposal_ready",
      created_by: "local",
      created_at: "2026-07-23T00:00:00Z",
      ...overrides,
    },
    artifact: { type: "plan", id: "plan-panel", version: 3, revision_hash: "sha256:abcdef1234567890" },
    review: { title: "Plan Panel Review" },
  };
}

function comment(overrides: Partial<ArtifactComment> = {}): ArtifactComment {
  return {
    id: "comment-1",
    review_id: "review-1",
    author: "local",
    status: "open",
    anchor: { kind: "markdown_block", base_revision_hash: "sha256:x", heading_path: ["A"], quoted_text: "q" },
    body: "Please fix the security section.",
    ...overrides,
  };
}

function proposal(overrides: Partial<ArtifactRevisionProposal> = {}): ArtifactRevisionProposal {
  return {
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
      change_summary: "Tightened the security section.",
    },
    results: {
      addressed_comments: 1,
      partially_addressed_comments: 0,
      unresolved_comments: 0,
      validation_status: "passed",
      comment_resolutions: [
        { comment_id: "comment-1", status: "addressed", changed_block_ids: ["security-model"] },
      ],
    },
    ...overrides,
  };
}

const sampleDiffLines: reviewApi.DiffLine[] = [
  { Kind: "equal", Text: "# Plan Panel" },
  { Kind: "removed", Text: "Default binding: 127.0.0.1 only" },
  { Kind: "added", Text: "Default binding: 127.0.0.1 only, LAN mode opt-in" },
];

function mockAll(overrides: { proposal?: ArtifactRevisionProposal; comments?: ArtifactComment[] } = {}) {
  const p = overrides.proposal ?? proposal();
  vi.spyOn(reviewApi, "listProposals").mockResolvedValue({ items: [p] });
  vi.spyOn(reviewApi, "getProposalDiff").mockResolvedValue({
    lines: sampleDiffLines,
    summary: { added: 1, removed: 1 },
  });
  vi.spyOn(reviewApi, "getProposalValidation").mockResolvedValue({
    structural: { passed: true, issues: [] },
    compliance: { passed: true, issues: [], unresolved_comment_ids: [] },
  });
  return p;
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("ProposalReview", () => {
  it("renders the diff with a +N/-M summary badge", async () => {
    mockAll();
    render(ProposalReview, {
      props: {
        reviewId: "review-1",
        review: review(),
        comments: [comment()],
        isDesktop: true,
        onreviewChanged: vi.fn(),
      },
    });

    await waitFor(() => expect(screen.getByTestId("diff-summary-badge")).toBeTruthy());
    const badge = screen.getByTestId("diff-summary-badge");
    expect(badge.textContent).toContain("+1");
    expect(badge.textContent).toContain("-1");
    expect(screen.getByTestId("diff-viewer")).toBeTruthy();
  });

  it("collapses long unchanged runs in the rendered diff", async () => {
    const p = proposal();
    vi.spyOn(reviewApi, "listProposals").mockResolvedValue({ items: [p] });
    vi.spyOn(reviewApi, "getProposalDiff").mockResolvedValue({
      lines: Array.from({ length: 10 }, (_, i) => ({ Kind: "equal" as const, Text: `line ${i}` })),
      summary: { added: 0, removed: 0 },
    });
    vi.spyOn(reviewApi, "getProposalValidation").mockResolvedValue({
      structural: { passed: true, issues: [] },
      compliance: { passed: true, issues: [] },
    });

    render(ProposalReview, {
      props: { reviewId: "review-1", review: review(), comments: [], isDesktop: false, onreviewChanged: vi.fn() },
    });

    await waitFor(() => expect(screen.getByTestId("diff-collapsed-toggle")).toBeTruthy());
  });

  it("filters the diff via the search box", async () => {
    mockAll();
    render(ProposalReview, {
      props: {
        reviewId: "review-1",
        review: review(),
        comments: [comment()],
        isDesktop: false,
        onreviewChanged: vi.fn(),
      },
    });

    await waitFor(() => expect(screen.getByTestId("diff-search")).toBeTruthy());
    await fireEvent.input(screen.getByTestId("diff-search"), { target: { value: "LAN mode" } });
    expect(screen.getByText(/LAN mode/)).toBeTruthy();
    expect(screen.queryByText("# Plan Panel")).toBeNull();
  });

  it("matches comments to their resolutions and shows a status badge", async () => {
    mockAll();
    render(ProposalReview, {
      props: {
        reviewId: "review-1",
        review: review(),
        comments: [comment()],
        isDesktop: true,
        onreviewChanged: vi.fn(),
      },
    });

    await waitFor(() => expect(screen.getByTestId("comment-resolution-list")).toBeTruthy());
    const item = screen.getByTestId("comment-resolution-item");
    expect(within(item).getByText("Please fix the security section.")).toBeTruthy();
    expect(within(item).getByText("Addressed")).toBeTruthy();
    expect(within(item).getByText(/security-model/)).toBeTruthy();
  });

  it("flags a comment with no matching resolution as unresolved", async () => {
    const p = proposal({
      results: {
        addressed_comments: 0,
        partially_addressed_comments: 0,
        unresolved_comments: 1,
        validation_status: "passed",
        comment_resolutions: [],
      },
    });
    vi.spyOn(reviewApi, "listProposals").mockResolvedValue({ items: [p] });
    vi.spyOn(reviewApi, "getProposalDiff").mockResolvedValue({ lines: [], summary: { added: 0, removed: 0 } });
    vi.spyOn(reviewApi, "getProposalValidation").mockResolvedValue({
      structural: { passed: true, issues: [] },
      compliance: { passed: false, issues: [], unresolved_comment_ids: ["comment-1"] },
    });

    render(ProposalReview, {
      props: {
        reviewId: "review-1",
        review: review(),
        comments: [comment()],
        isDesktop: true,
        onreviewChanged: vi.fn(),
      },
    });

    await waitFor(() => expect(screen.getByTestId("comment-resolution-item")).toBeTruthy());
    const item = screen.getByTestId("comment-resolution-item");
    expect(within(item).getByText("Unresolved")).toBeTruthy();
    expect(item.className).toContain("unresolved");
  });

  it("shows an explanation for a rejected resolution", async () => {
    const p = proposal({
      results: {
        validation_status: "passed",
        comment_resolutions: [
          { comment_id: "comment-1", status: "rejected", explanation: "Out of scope for this review." },
        ],
      },
    });
    mockAll({ proposal: p });

    render(ProposalReview, {
      props: {
        reviewId: "review-1",
        review: review(),
        comments: [comment()],
        isDesktop: true,
        onreviewChanged: vi.fn(),
      },
    });

    await waitFor(() => expect(screen.getByText("Out of scope for this review.")).toBeTruthy());
  });

  it("renders structural and compliance validation reports with pass/fail badges", async () => {
    const p = proposal();
    vi.spyOn(reviewApi, "listProposals").mockResolvedValue({ items: [p] });
    vi.spyOn(reviewApi, "getProposalDiff").mockResolvedValue({ lines: [], summary: { added: 0, removed: 0 } });
    vi.spyOn(reviewApi, "getProposalValidation").mockResolvedValue({
      structural: { passed: false, issues: [{ check: "balanced_fences", message: "unbalanced fences" }] },
      compliance: { passed: true, issues: [] },
    });

    render(ProposalReview, {
      props: { reviewId: "review-1", review: review(), comments: [], isDesktop: true, onreviewChanged: vi.fn() },
    });

    await waitFor(() => expect(screen.getByText("Structural: failed")).toBeTruthy());
    expect(screen.getByText(/unbalanced fences/)).toBeTruthy();
    expect(screen.getByText("Review compliance: passed")).toBeTruthy();
    expect(screen.getByText("Overall: failed")).toBeTruthy();
  });

  describe("accept", () => {
    it("accepts successfully and calls onreviewChanged with the updated review", async () => {
      mockAll();
      const acceptedReview = review({ status: "accepted" });
      vi.spyOn(reviewApi, "acceptProposal").mockResolvedValue({ review: acceptedReview, new_version: {} });
      const onreviewChanged = vi.fn();

      render(ProposalReview, {
        props: { reviewId: "review-1", review: review(), comments: [comment()], isDesktop: true, onreviewChanged },
      });

      await waitFor(() => expect(screen.getByTestId("accept-button")).toBeTruthy());
      await fireEvent.click(screen.getByTestId("accept-button"));
      const dialog = screen.getByRole("dialog");
      await fireEvent.click(within(dialog).getByText("Accept Proposal"));

      await waitFor(() => expect(reviewApi.acceptProposal).toHaveBeenCalledWith("review-1", "1"));
      expect(onreviewChanged).toHaveBeenCalledWith(acceptedReview);
    });

    it("disables Accept and explains why when validation failed", async () => {
      const p = proposal({
        results: {
          validation_status: "failed",
          comment_resolutions: [{ comment_id: "comment-1", status: "addressed", changed_block_ids: ["a"] }],
        },
      });
      mockAll({ proposal: p });

      render(ProposalReview, {
        props: { reviewId: "review-1", review: review(), comments: [comment()], isDesktop: true, onreviewChanged: vi.fn() },
      });

      await waitFor(() => expect(screen.getByTestId("accept-button")).toBeTruthy());
      expect((screen.getByTestId("accept-button") as HTMLButtonElement).disabled).toBe(true);
      expect(screen.getByTestId("accept-disabled-reason")).toBeTruthy();
    });

    it("shows the rebase call-to-action on a 409 conflict from accept", async () => {
      mockAll();
      vi.spyOn(reviewApi, "acceptProposal").mockRejectedValue(
        new reviewApi.ApiError(409, "api: canonical artifact changed since this proposal's base"),
      );

      render(ProposalReview, {
        props: { reviewId: "review-1", review: review(), comments: [comment()], isDesktop: true, onreviewChanged: vi.fn() },
      });

      await waitFor(() => expect(screen.getByTestId("accept-button")).toBeTruthy());
      await fireEvent.click(screen.getByTestId("accept-button"));
      const dialog = screen.getByRole("dialog");
      await fireEvent.click(within(dialog).getByText("Accept Proposal"));

      await waitFor(() => expect(screen.getByTestId("conflict-banner")).toBeTruthy());
      expect(screen.getByText(/Rebase to re-anchor/)).toBeTruthy();
    });
  });

  describe("reject", () => {
    it("rejects with a non-destructive confirm dialog", async () => {
      mockAll();
      const rejectedReview = review({ status: "rejected" });
      vi.spyOn(reviewApi, "rejectProposal").mockResolvedValue(rejectedReview);
      const onreviewChanged = vi.fn();

      render(ProposalReview, {
        props: { reviewId: "review-1", review: review(), comments: [comment()], isDesktop: true, onreviewChanged },
      });

      await waitFor(() => expect(screen.getByTestId("reject-button")).toBeTruthy());
      await fireEvent.click(screen.getByTestId("reject-button"));
      expect(screen.getByText(/canonical artifact is never touched/)).toBeTruthy();

      const dialog = screen.getByRole("dialog");
      await fireEvent.click(within(dialog).getByText("Reject Proposal"));

      await waitFor(() => expect(reviewApi.rejectProposal).toHaveBeenCalledWith("review-1", "1"));
      expect(onreviewChanged).toHaveBeenCalledWith(rejectedReview);
    });
  });

  describe("request changes", () => {
    it("opens an instruction textarea and posts on submit", async () => {
      mockAll();
      const updatedReview = review({ status: "revision_requested" });
      vi.spyOn(reviewApi, "requestChanges").mockResolvedValue({
        review: updatedReview,
        run: { run_id: "run-2", parent_task_id: "run-2" },
      });
      const onreviewChanged = vi.fn();

      render(ProposalReview, {
        props: { reviewId: "review-1", review: review(), comments: [comment()], isDesktop: true, onreviewChanged },
      });

      await waitFor(() => expect(screen.getByTestId("request-changes-button")).toBeTruthy());
      await fireEvent.click(screen.getByTestId("request-changes-button"));

      await fireEvent.input(screen.getByTestId("request-changes-input"), {
        target: { value: "Please also cover retries." },
      });
      const dialog = screen.getByRole("dialog");
      await fireEvent.click(within(dialog).getByText("Request Changes"));

      await waitFor(() =>
        expect(reviewApi.requestChanges).toHaveBeenCalledWith("review-1", "1", "Please also cover retries."),
      );
      expect(onreviewChanged).toHaveBeenCalledWith(updatedReview);
    });
  });

  describe("conflicted / rebase", () => {
    it("shows a conflict banner and rebases via the rebase endpoint", async () => {
      mockAll();
      const draftReview = review({ status: "draft" });
      vi.spyOn(reviewApi, "rebaseReview").mockResolvedValue(draftReview);
      const onreviewChanged = vi.fn();

      render(ProposalReview, {
        props: {
          reviewId: "review-1",
          review: review({ status: "conflicted" }),
          comments: [comment()],
          isDesktop: true,
          onreviewChanged,
        },
      });

      await waitFor(() => expect(screen.getByTestId("conflict-banner")).toBeTruthy());
      expect(screen.getByText(/changed since this review's base version/)).toBeTruthy();
      await fireEvent.click(screen.getByText("Rebase onto latest version"));

      await waitFor(() => expect(reviewApi.rebaseReview).toHaveBeenCalledWith("review-1"));
      expect(onreviewChanged).toHaveBeenCalledWith(draftReview);
    });

    it("hides accept/reject/request-changes actions while conflicted", async () => {
      mockAll();
      render(ProposalReview, {
        props: {
          reviewId: "review-1",
          review: review({ status: "conflicted" }),
          comments: [comment()],
          isDesktop: true,
          onreviewChanged: vi.fn(),
        },
      });

      await waitFor(() => expect(screen.getByTestId("conflict-banner")).toBeTruthy());
      expect(screen.queryByTestId("accept-button")).toBeNull();
      expect(screen.queryByTestId("reject-button")).toBeNull();
      expect(screen.queryByTestId("request-changes-button")).toBeNull();
    });
  });

  // Responsive "visual regression" per apy.7's hardening pass: jsdom has
  // no layout engine, so pixel screenshots aren't feasible (see the plan
  // doc's own acknowledgment of that limitation). This asserts the right
  // DOM structure for each of the plan's four breakpoints via the
  // isDesktop seam ProposalReview already exposes - 360/768px map to the
  // unified-diff/mobile branch, 1024/wide map to the side-by-side/desktop
  // branch - matching DiffViewer's own breakpoint tests.
  describe("breakpoint-driven layout (360/768 -> unified diff, 1024/wide -> side-by-side diff)", () => {
    it.each([
      { label: "360px (mobile)", isDesktop: false },
      { label: "768px (tablet)", isDesktop: false },
    ])("renders the unified diff mode at $label", async ({ isDesktop }) => {
      mockAll();
      render(ProposalReview, {
        props: { reviewId: "review-1", review: review(), comments: [comment()], isDesktop, onreviewChanged: vi.fn() },
      });

      await waitFor(() => expect(screen.getByTestId("diff-viewer")).toBeTruthy());
      expect(screen.getByTestId("diff-unified")).toBeTruthy();
      expect(screen.queryByTestId("diff-side-by-side")).toBeNull();
    });

    it.each([
      { label: "1024px (desktop)", isDesktop: true },
      { label: "1440px+ (wide)", isDesktop: true },
    ])("renders the side-by-side diff mode at $label", async ({ isDesktop }) => {
      mockAll();
      render(ProposalReview, {
        props: { reviewId: "review-1", review: review(), comments: [comment()], isDesktop, onreviewChanged: vi.fn() },
      });

      await waitFor(() => expect(screen.getByTestId("diff-viewer")).toBeTruthy());
      expect(screen.getByTestId("diff-side-by-side")).toBeTruthy();
      expect(screen.queryByTestId("diff-unified")).toBeNull();
    });

    it("keeps Accept/Reject/Request Changes reachable at every breakpoint via StickyActionBar", async () => {
      mockAll();
      for (const isDesktop of [false, true]) {
        const { unmount } = render(ProposalReview, {
          props: {
            reviewId: "review-1",
            review: review(),
            comments: [comment()],
            isDesktop,
            onreviewChanged: vi.fn(),
          },
        });
        await waitFor(() => expect(screen.getByTestId("accept-button")).toBeTruthy());
        expect(screen.getByTestId("reject-button")).toBeTruthy();
        expect(screen.getByTestId("request-changes-button")).toBeTruthy();
        unmount();
      }
    });
  });

  describe("revision_requested (in-flight)", () => {
    it("shows the previous proposal read-only with an in-flight banner and no actions", async () => {
      mockAll();
      render(ProposalReview, {
        props: {
          reviewId: "review-1",
          review: review({ status: "revision_requested" }),
          comments: [comment()],
          isDesktop: true,
          onreviewChanged: vi.fn(),
        },
      });

      await waitFor(() => expect(screen.getByTestId("revision-in-flight-banner")).toBeTruthy());
      expect(screen.getByTestId("diff-viewer")).toBeTruthy();
      expect(screen.queryByTestId("accept-button")).toBeNull();
    });
  });
});
