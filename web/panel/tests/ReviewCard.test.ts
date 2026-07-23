import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it, vi } from "vitest";
import ReviewCard from "../src/lib/components/cards/ReviewCard.svelte";

describe("ReviewCard", () => {
  it("renders without crashing and shows title, artifact id/version, and comment count", () => {
    render(ReviewCard, {
      props: {
        title: "Add checkout API contract",
        statusVariant: "info",
        statusLabel: "In review",
        artifactId: "artifact-42",
        version: 3,
        commentCount: 5,
      },
    });
    expect(screen.getByText("Add checkout API contract")).toBeTruthy();
    expect(screen.getByText("In review")).toBeTruthy();
    expect(screen.getByText("artifact-42 · v3")).toBeTruthy();
    expect(screen.getByText("5 comments")).toBeTruthy();
  });

  it("uses singular 'comment' for a count of 1", () => {
    render(ReviewCard, {
      props: {
        title: "Fix typo",
        statusVariant: "success",
        statusLabel: "Approved",
        artifactId: "artifact-1",
        version: 1,
        commentCount: 1,
      },
    });
    expect(screen.getByText("1 comment")).toBeTruthy();
  });

  it("calls onselect when the title is clicked", async () => {
    const onselect = vi.fn();
    render(ReviewCard, {
      props: {
        title: "Clickable review",
        statusVariant: "warning",
        statusLabel: "Needs input",
        artifactId: "artifact-9",
        version: 2,
        commentCount: 0,
        onselect,
      },
    });
    await fireEvent.click(screen.getByRole("button", { name: "Clickable review" }));
    expect(onselect).toHaveBeenCalledOnce();
  });
});
