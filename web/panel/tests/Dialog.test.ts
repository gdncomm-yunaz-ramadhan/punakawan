import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import DialogHarness from "./fixtures/DialogHarness.svelte";

describe("Dialog", () => {
  it("is closed until its trigger opens it", async () => {
    render(DialogHarness);
    expect(screen.queryByRole("dialog")).toBeNull();

    await fireEvent.click(screen.getByRole("button", { name: "Open" }));
    expect(screen.getByRole("dialog", { name: "Example dialog" })).toBeTruthy();
    expect(screen.getByText("Dialog body")).toBeTruthy();
  });

  it("closes when the close button is clicked", async () => {
    render(DialogHarness);
    await fireEvent.click(screen.getByRole("button", { name: "Open" }));

    await fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("closes on backdrop click", async () => {
    render(DialogHarness);
    await fireEvent.click(screen.getByRole("button", { name: "Open" }));

    await fireEvent.click(screen.getAllByRole("presentation")[0]);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("closes on Escape", async () => {
    render(DialogHarness);
    await fireEvent.click(screen.getByRole("button", { name: "Open" }));

    await fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
