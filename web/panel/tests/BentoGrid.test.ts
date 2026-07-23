import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import BentoGridHarness from "./fixtures/BentoGridHarness.svelte";

describe("BentoGrid", () => {
  it("renders without crashing and renders its children inside a grid container", () => {
    render(BentoGridHarness);
    expect(screen.getByTestId("bento-grid-content")).toBeTruthy();
    expect(document.querySelector(".bento-grid")).toBeTruthy();
  });
});
