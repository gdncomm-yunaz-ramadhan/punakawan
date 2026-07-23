import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import BottomSheetHarness from "./fixtures/BottomSheetHarness.svelte";

describe("BottomSheet", () => {
  it("is closed until its trigger opens it", async () => {
    render(BottomSheetHarness);
    expect(screen.queryByRole("dialog")).toBeNull();

    await fireEvent.click(screen.getByRole("button", { name: "Open" }));
    expect(screen.getByRole("dialog", { name: "Example sheet" })).toBeTruthy();
    expect(screen.getByText("Sheet body")).toBeTruthy();
  });

  it("closes when the close button is clicked", async () => {
    render(BottomSheetHarness);
    await fireEvent.click(screen.getByRole("button", { name: "Open" }));

    await fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("closes on backdrop click", async () => {
    render(BottomSheetHarness);
    await fireEvent.click(screen.getByRole("button", { name: "Open" }));

    await fireEvent.click(screen.getByRole("presentation"));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("closes on Escape", async () => {
    render(BottomSheetHarness);
    await fireEvent.click(screen.getByRole("button", { name: "Open" }));

    await fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
