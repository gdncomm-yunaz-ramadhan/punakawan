import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import StickyActionBarHarness from "./fixtures/StickyActionBarHarness.svelte";

describe("StickyActionBar", () => {
  it("renders its slotted action buttons", () => {
    render(StickyActionBarHarness);

    expect(screen.getByRole("button", { name: "Primary" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Secondary" })).toBeTruthy();
  });
});
