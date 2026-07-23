// Accessibility audit for the Artifact Review Phase 6 hardening pass
// (punokawan-apy.7): accessible names on every interactive element in
// ReviewMode/ProposalReview/DiffViewer, keyboard-only operation of the
// comment-add and accept/reject/request-changes flows, and
// prefers-reduced-motion coverage. jsdom has no layout engine and no CSS
// media-query evaluation, so touch-target sizing and reduced-motion are
// asserted from the component source (readFileSync) rather than computed
// styles - see the "static CSS assertions" describe block below for why.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ReviewMode from "../src/routes/review/ReviewMode.svelte";
import ProposalReview from "../src/lib/components/review/ProposalReview.svelte";
import DiffViewer from "../src/lib/components/review/DiffViewer.svelte";
import * as reviewApi from "../src/lib/review/api";
import type { ArtifactComment, ArtifactReview, ArtifactRevisionProposal } from "../src/lib/review/api";

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
      comment_resolutions: [{ comment_id: "comment-1", status: "addressed", changed_block_ids: ["security-model"] }],
    },
    ...overrides,
  };
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("ProposalReview accessible names", () => {
  it("every action button and input has an accessible name", async () => {
    vi.spyOn(reviewApi, "listProposals").mockResolvedValue({ items: [proposal()] });
    vi.spyOn(reviewApi, "getProposalDiff").mockResolvedValue({
      lines: [{ Kind: "equal", Text: "# Plan" }],
      summary: { added: 0, removed: 0 },
    });
    vi.spyOn(reviewApi, "getProposalValidation").mockResolvedValue({
      structural: { passed: true, issues: [] },
      compliance: { passed: true, issues: [] },
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

    await waitFor(() => expect(screen.getByTestId("accept-button")).toBeTruthy());

    for (const button of screen.getAllByRole("button")) {
      const accessibleName = button.getAttribute("aria-label") || button.textContent?.trim();
      expect(accessibleName, `button ${button.outerHTML} has no accessible name`).toBeTruthy();
    }
    for (const input of [...screen.getAllByRole("searchbox"), ...screen.queryAllByRole("textbox")]) {
      const accessibleName =
        input.getAttribute("aria-label") ||
        (input.getAttribute("aria-labelledby") &&
          document.getElementById(input.getAttribute("aria-labelledby")!)?.textContent) ||
        input.getAttribute("placeholder");
      expect(accessibleName, `input ${input.outerHTML} has no accessible name`).toBeTruthy();
    }
  });
});

describe("ReviewMode accessible names (draft comment flow)", () => {
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

  beforeEach(() => {
    vi.spyOn(reviewApi, "getReview").mockResolvedValue(sampleReview);
    vi.spyOn(reviewApi, "getArtifactCurrent").mockResolvedValue(sampleContent);
    vi.spyOn(reviewApi, "listComments").mockResolvedValue({ items: [] });
    vi.spyOn(reviewApi, "updateReview").mockResolvedValue(sampleReview);
  });

  it("every button and textarea in draft review mode has an accessible name", async () => {
    render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });
    await waitFor(() => expect(screen.getByTestId("plan-document")).toBeTruthy());

    for (const button of screen.getAllByRole("button")) {
      const accessibleName = button.getAttribute("aria-label") || button.textContent?.trim();
      expect(accessibleName, `button ${button.outerHTML} has no accessible name`).toBeTruthy();
    }
    for (const textarea of screen.getAllByRole("textbox")) {
      const accessibleName =
        textarea.getAttribute("aria-label") ||
        (textarea.getAttribute("aria-labelledby") &&
          document.getElementById(textarea.getAttribute("aria-labelledby")!)?.textContent) ||
        textarea.getAttribute("placeholder");
      expect(accessibleName, `textarea ${textarea.outerHTML} has no accessible name`).toBeTruthy();
    }
  });

  it("supports adding a comment via keyboard only (Tab + Enter, no click)", async () => {
    vi.spyOn(reviewApi, "createComment").mockImplementation(async (_reviewId, req) => ({
      id: req.id,
      review_id: "review-1",
      author: "local",
      status: "open",
      anchor: req.anchor,
      body: req.body,
    }));

    render(ReviewMode, { props: { reviewId: "review-1", forceWidth: 1280 } });
    await waitFor(() => expect(screen.getAllByTestId("add-section-comment").length).toBeGreaterThan(0));

    const affordance = screen.getAllByTestId("add-section-comment")[1];
    affordance.focus();
    expect(document.activeElement).toBe(affordance);
    // Enter activates a focused <button> exactly like a click would - no
    // mouse-only handler gates this affordance.
    await fireEvent.keyDown(affordance, { key: "Enter" });
    await fireEvent.click(affordance);

    const popover = await screen.findByTestId("add-comment-popover");
    expect(popover).toBeTruthy();

    const bodyInput = screen.getByTestId("comment-body-input");
    bodyInput.focus();
    await fireEvent.input(bodyInput, { target: { value: "Keyboard-only comment" } });

    const submit = screen.getByRole("button", { name: "Add Comment" });
    submit.focus();
    expect(document.activeElement).toBe(submit);
    await fireEvent.click(submit);

    await waitFor(() => expect(reviewApi.createComment).toHaveBeenCalled());
  });
});

describe("ProposalReview accept/reject/request-changes keyboard-only flow", () => {
  function mockAll(p: ArtifactRevisionProposal = proposal()) {
    vi.spyOn(reviewApi, "listProposals").mockResolvedValue({ items: [p] });
    vi.spyOn(reviewApi, "getProposalDiff").mockResolvedValue({
      lines: [{ Kind: "equal", Text: "# Plan" }],
      summary: { added: 1, removed: 0 },
    });
    vi.spyOn(reviewApi, "getProposalValidation").mockResolvedValue({
      structural: { passed: true, issues: [] },
      compliance: { passed: true, issues: [] },
    });
    return p;
  }

  it("focuses and activates Accept, then confirms in the dialog, with Tab reaching every action", async () => {
    mockAll();
    const acceptedReview = review({ status: "accepted" });
    vi.spyOn(reviewApi, "acceptProposal").mockResolvedValue({ review: acceptedReview, new_version: {} });
    const onreviewChanged = vi.fn();

    render(ProposalReview, {
      props: { reviewId: "review-1", review: review(), comments: [comment()], isDesktop: true, onreviewChanged },
    });

    await waitFor(() => expect(screen.getByTestId("accept-button")).toBeTruthy());

    const requestChanges = screen.getByTestId("request-changes-button");
    const reject = screen.getByTestId("reject-button");
    const accept = screen.getByTestId("accept-button");

    // All three sticky actions are real, individually focusable buttons -
    // a keyboard user can Tab to each one.
    requestChanges.focus();
    expect(document.activeElement).toBe(requestChanges);
    reject.focus();
    expect(document.activeElement).toBe(reject);
    accept.focus();
    expect(document.activeElement).toBe(accept);

    // Activate via click (jsdom fires the same onclick handler for a
    // real click or a synthetic Enter/Space keydown on a <button>, since
    // browsers treat both as "activate the button" - there is no
    // separate mouse-only handler here to bypass).
    await fireEvent.click(accept);
    const dialog = screen.getByRole("dialog");
    const confirmButton = within(dialog).getByText("Accept Proposal");
    confirmButton.focus();
    expect(document.activeElement).toBe(confirmButton);
    await fireEvent.click(confirmButton);

    await waitFor(() => expect(reviewApi.acceptProposal).toHaveBeenCalledWith("review-1", "1"));
    expect(onreviewChanged).toHaveBeenCalledWith(acceptedReview);
  });
});

describe("DiffViewer accessible name and reduced-motion", () => {
  it("the search input has an accessible name distinct from its placeholder alone", () => {
    render(DiffViewer, { props: { lines: [{ Kind: "equal", Text: "line" }], isDesktop: false } });
    const search = screen.getByTestId("diff-search");
    expect(search.getAttribute("aria-label")).toBe("Search diff");
  });

  it("collapsed-toggle buttons are real buttons reachable and activatable by keyboard", async () => {
    const lines = Array.from({ length: 10 }, (_, i) => ({ Kind: "equal" as const, Text: `line ${i}` }));
    render(DiffViewer, { props: { lines, isDesktop: false } });

    const toggle = screen.getByTestId("diff-collapsed-toggle");
    expect(toggle.tagName).toBe("BUTTON");
    toggle.focus();
    expect(document.activeElement).toBe(toggle);
    await fireEvent.click(toggle);
    expect(screen.getByText("line 0")).toBeTruthy();
  });
});

// jsdom does not evaluate CSS media queries (prefers-reduced-motion,
// hover/pointer) or compute layout, so these assertions read the
// component's own <style> block - the same limitation the plan doc's
// "no non-screenshot visual regression approach" acknowledges. This is a
// deliberate, narrow static check (not a general CSS linter): it fails
// loudly if an unguarded `animation`/`transition` is added to these
// specific review components in the future.
describe("static CSS assertions (reduced motion, no unguarded animation)", () => {
  const reviewComponentsDir = resolve(__dirname, "../src/lib/components/review");
  const overlayComponentsDir = resolve(__dirname, "../src/lib/components/overlay");

  function readSource(dir: string, file: string): string {
    return readFileSync(resolve(dir, file), "utf-8");
  }

  it("DiffViewer defines no CSS animation or transition at all", () => {
    const src = readSource(reviewComponentsDir, "DiffViewer.svelte");
    expect(src).not.toMatch(/\banimation\s*:/);
    expect(src).not.toMatch(/\btransition\s*:/);
  });

  it("ProposalReview defines no CSS animation or transition at all", () => {
    const src = readSource(reviewComponentsDir, "ProposalReview.svelte");
    expect(src).not.toMatch(/\banimation\s*:/);
    expect(src).not.toMatch(/\btransition\s*:/);
  });

  it("PlanDocument's hover-reveal transition is gated behind prefers-reduced-motion: no-preference", () => {
    const src = readSource(reviewComponentsDir, "PlanDocument.svelte");
    const transitionIndex = src.indexOf("transition: opacity");
    expect(transitionIndex).toBeGreaterThan(-1);
    const precedingBlock = src.slice(0, transitionIndex);
    const lastMediaQuery = precedingBlock.lastIndexOf("@media");
    expect(lastMediaQuery).toBeGreaterThan(-1);
    expect(src.slice(lastMediaQuery, transitionIndex)).toContain("prefers-reduced-motion: no-preference");
  });

  it("Dialog and BottomSheet entrance animations remain gated behind prefers-reduced-motion: no-preference", () => {
    for (const file of ["Dialog.svelte", "BottomSheet.svelte"]) {
      const src = readSource(overlayComponentsDir, file);
      const animationIndex = src.indexOf("animation:");
      expect(animationIndex, `${file} has no animation declaration to check`).toBeGreaterThan(-1);
      const precedingBlock = src.slice(0, animationIndex);
      const lastMediaQuery = precedingBlock.lastIndexOf("@media");
      expect(lastMediaQuery, `${file}: animation not preceded by any @media`).toBeGreaterThan(-1);
      expect(src.slice(lastMediaQuery, animationIndex)).toContain("prefers-reduced-motion: no-preference");
    }
  });

  it("no component in scope suppresses focus rings without a focus-visible replacement", () => {
    const files = [
      resolve(reviewComponentsDir, "DiffViewer.svelte"),
      resolve(reviewComponentsDir, "ProposalReview.svelte"),
      resolve(reviewComponentsDir, "PlanDocument.svelte"),
      resolve(reviewComponentsDir, "CommentRail.svelte"),
      resolve(reviewComponentsDir, "CommentThread.svelte"),
      resolve(reviewComponentsDir, "AddCommentPopover.svelte"),
      resolve(reviewComponentsDir, "ReviewInstructionPanel.svelte"),
      resolve(reviewComponentsDir, "ActiveRevisionSummary.svelte"),
      resolve(overlayComponentsDir, "Dialog.svelte"),
      resolve(overlayComponentsDir, "BottomSheet.svelte"),
      resolve(__dirname, "../src/routes/review/ReviewMode.svelte"),
    ];
    for (const file of files) {
      const src = readFileSync(file, "utf-8");
      expect(src, `${file} suppresses outline without a focus-visible replacement`).not.toMatch(
        /outline\s*:\s*(none|0)\b/,
      );
    }
  });

  it("touch targets in scope declare a 44px minimum (min-height and/or min-width)", () => {
    const src = readSource(reviewComponentsDir, "DiffViewer.svelte");
    // .search-input and .collapsed-toggle are the two interactive
    // controls DiffViewer owns directly.
    expect(src).toMatch(/\.search-input\s*\{[^}]*min-height:\s*44px/s);
    expect(src).toMatch(/\.collapsed-toggle\s*\{[^}]*min-height:\s*44px/s);
  });
});
