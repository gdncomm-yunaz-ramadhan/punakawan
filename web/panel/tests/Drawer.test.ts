import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import DrawerHarness from "./fixtures/DrawerHarness.svelte";

describe("Drawer", () => {
  it("is closed until its trigger opens it", async () => {
    render(DrawerHarness);
    expect(screen.queryByLabelText("Example drawer")).toBeNull();

    await fireEvent.click(screen.getByRole("button", { name: "Open" }));
    expect(screen.getByLabelText("Example drawer")).toBeTruthy();
    expect(screen.getByText("Drawer body")).toBeTruthy();
  });

  it("closes when the close button is clicked", async () => {
    render(DrawerHarness);
    await fireEvent.click(screen.getByRole("button", { name: "Open" }));

    await fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByLabelText("Example drawer")).toBeNull();
  });

  it("closes on backdrop click", async () => {
    render(DrawerHarness);
    await fireEvent.click(screen.getByRole("button", { name: "Open" }));

    await fireEvent.click(screen.getByRole("presentation"));
    expect(screen.queryByLabelText("Example drawer")).toBeNull();
  });

  it("closes on Escape", async () => {
    render(DrawerHarness);
    await fireEvent.click(screen.getByRole("button", { name: "Open" }));

    await fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByLabelText("Example drawer")).toBeNull();
  });
});
