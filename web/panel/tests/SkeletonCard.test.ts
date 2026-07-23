import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import SkeletonCard from "../src/lib/components/cards/SkeletonCard.svelte";

describe("SkeletonCard", () => {
  it("renders without crashing as a status region", () => {
    render(SkeletonCard);
    expect(screen.getByRole("status", { name: "Loading" })).toBeTruthy();
  });

  it("renders the requested number of skeleton lines", () => {
    render(SkeletonCard, { props: { lines: 5 } });
    expect(document.querySelectorAll(".skeleton-line").length).toBe(5);
  });
});
