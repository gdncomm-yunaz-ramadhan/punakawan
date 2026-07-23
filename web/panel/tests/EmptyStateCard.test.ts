import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import EmptyStateCard from "../src/lib/components/cards/EmptyStateCard.svelte";

describe("EmptyStateCard", () => {
  it("renders without crashing with default copy", () => {
    render(EmptyStateCard);
    expect(screen.getByText("Nothing here yet")).toBeTruthy();
  });

  it("renders custom title and message", () => {
    render(EmptyStateCard, { props: { title: "No reviews", message: "Nothing needs your attention." } });
    expect(screen.getByText("No reviews")).toBeTruthy();
    expect(screen.getByText("Nothing needs your attention.")).toBeTruthy();
  });
});
