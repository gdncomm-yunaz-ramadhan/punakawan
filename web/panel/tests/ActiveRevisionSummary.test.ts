import { fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { describe, expect, it, vi } from "vitest";
import ActiveRevisionSummary from "../src/lib/components/review/ActiveRevisionSummary.svelte";
import type { ArtifactComment, ArtifactRevisionRequest, ReviewStatus, RunReference } from "../src/lib/review/api";

// Reused, untouched chart component pulls in a real Chart.js instance -
// stub it out exactly like CommentResolutionChart.test.ts does, since
// jsdom has no canvas support.
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

function comment(overrides: Partial<ArtifactComment> = {}): ArtifactComment {
  return {
    id: "comment-1",
    review_id: "review-1",
    author: "local",
    status: "open",
    anchor: { kind: "markdown_block", base_revision_hash: "sha256:x", heading_path: ["A"], quoted_text: "q" },
    body: "a comment",
    ...overrides,
  };
}

const revisionRequest: ArtifactRevisionRequest = {
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

const run: RunReference = { run_id: "revision-a1b2c3d4e5f6", parent_task_id: "revision-a1b2c3d4e5f6" };

const baseProps = {
  baseVersion: 3,
  baseRevisionHash: "sha256:abcdef1234567890",
  comments: [comment()],
  revisionRequest,
  run,
};

describe("ActiveRevisionSummary", () => {
  it.each<[ReviewStatus, string]>([
    ["submitted", "info"],
    ["queued", "info"],
    ["revising", "info"],
    ["awaiting_clarification", "warning"],
    ["proposal_ready", "success"],
    ["revision_requested", "info"],
    ["accepted", "success"],
    ["rejected", "danger"],
    ["cancelled", "warning"],
    ["failed", "danger"],
    ["conflicted", "warning"],
  ])("renders status %s with a sensible label and variant", async (status, expectedVariant) => {
    render(ActiveRevisionSummary, { props: { ...baseProps, status } });

    const badges = screen.getAllByText(status.replace(/_/g, " "));
    expect(badges.length).toBeGreaterThan(0);
    const variantEl = document.querySelector(`.status-variant-${expectedVariant}`);
    expect(variantEl).toBeTruthy();
  });

  it("shows the truncated base revision hash and version", () => {
    render(ActiveRevisionSummary, { props: { ...baseProps, status: "queued" } });
    expect(screen.getByText(/v3/)).toBeTruthy();
    expect(screen.getByText(/abcdef123456…/)).toBeTruthy();
  });

  it("shows revision request submitted_at/submitted_by and the run id when present", () => {
    render(ActiveRevisionSummary, { props: { ...baseProps, status: "queued" } });
    expect(screen.getByText(/local/)).toBeTruthy();
    expect(screen.getByText(/revision-a1b2c3d4e5f6/)).toBeTruthy();
  });

  it("shows an empty state when no revision_request exists yet", () => {
    render(ActiveRevisionSummary, { props: { ...baseProps, status: "queued", revisionRequest: null, run: null } });
    expect(screen.getByText("No revision request yet")).toBeTruthy();
  });

  it("always shows the execution-evidence empty state (no evidence source wired up yet)", () => {
    render(ActiveRevisionSummary, { props: { ...baseProps, status: "queued" } });
    expect(screen.getByText("No execution evidence yet")).toBeTruthy();
    expect(screen.getByText(/hasn't been picked up by an agent/)).toBeTruthy();
  });

  it("renders the comment-status chart built from real comment data", async () => {
    const comments = [
      comment({ id: "c1", status: "open" }),
      comment({ id: "c2", status: "needs_clarification" }),
      comment({ id: "c3", status: "addressed" }),
      comment({ id: "c4", status: "resolved_by_user" }),
      comment({ id: "c5", status: "obsolete" }),
    ];
    render(ActiveRevisionSummary, { props: { ...baseProps, status: "queued", comments } });
    await waitFor(() => expect(screen.getByLabelText("Comment status breakdown")).toBeTruthy());
  });

  it("surfaces needs_clarification comments with an inline answer affordance", async () => {
    const clarification = comment({ id: "c2", status: "needs_clarification", body: "Which environment?" });
    const onanswerClarification = vi.fn();
    render(ActiveRevisionSummary, {
      props: {
        ...baseProps,
        status: "awaiting_clarification",
        comments: [comment(), clarification],
        onanswerClarification,
      },
    });

    expect(screen.getByTestId("clarification-section")).toBeTruthy();
    expect(screen.getByText("Which environment?")).toBeTruthy();

    await fireEvent.input(screen.getByTestId("clarification-input-c2"), { target: { value: "Staging" } });
    await fireEvent.click(screen.getByTestId("clarification-save-c2"));

    expect(onanswerClarification).toHaveBeenCalledWith("c2", "Staging");
  });

  it("does not show a clarification section when no comment needs it", () => {
    render(ActiveRevisionSummary, { props: { ...baseProps, status: "queued" } });
    expect(screen.queryByTestId("clarification-section")).toBeNull();
  });

  it("shows a Cancel Review action for a non-terminal status", () => {
    render(ActiveRevisionSummary, { props: { ...baseProps, status: "queued" } });
    expect(screen.getByTestId("cancel-review-button")).toBeTruthy();
  });

  it.each<ReviewStatus>(["accepted", "rejected", "cancelled", "failed"])(
    "hides the Cancel Review action for terminal status %s",
    (status) => {
      render(ActiveRevisionSummary, { props: { ...baseProps, status } });
      expect(screen.queryByTestId("cancel-review-button")).toBeNull();
    },
  );

  it("invokes oncancel when Cancel Review is clicked", async () => {
    const oncancel = vi.fn();
    render(ActiveRevisionSummary, { props: { ...baseProps, status: "queued", oncancel } });
    await fireEvent.click(screen.getByTestId("cancel-review-button"));
    expect(oncancel).toHaveBeenCalled();
  });
});
