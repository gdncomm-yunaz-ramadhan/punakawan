import { fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import BottomSheetHarness from "./fixtures/BottomSheetHarness.svelte";
import BottomSheetFocusHarness from "./fixtures/BottomSheetFocusHarness.svelte";

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

  describe("focus management (WCAG 2.4.3, §13.13 'mobile bottom sheets trap focus and restore it on close')", () => {
    // DOM order inside the sheet is Close (in .sheet-head), then the
    // caller's own content (First, Second) - so Close is the first
    // focusable element and Second is the last.
    it("moves focus into the sheet on open", async () => {
      render(BottomSheetFocusHarness);
      await fireEvent.click(screen.getByRole("button", { name: "Open" }));

      await waitFor(() => {
        expect(document.activeElement).toBe(screen.getByRole("button", { name: "Close" }));
      });
    });

    it("restores focus to the trigger after closing", async () => {
      render(BottomSheetFocusHarness);
      const openButton = screen.getByRole("button", { name: "Open" });
      // jsdom (unlike a real browser) doesn't move focus onto a button as
      // part of dispatching a click - focus it explicitly first so the
      // component sees a real "focus was on the trigger" precondition.
      openButton.focus();
      await fireEvent.click(openButton);
      await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Close" })));

      await fireEvent.click(screen.getByRole("button", { name: "Close" }));
      expect(document.activeElement).toBe(openButton);
    });

    it("restores focus to the trigger after closing via Escape", async () => {
      render(BottomSheetFocusHarness);
      const openButton = screen.getByRole("button", { name: "Open" });
      openButton.focus();
      await fireEvent.click(openButton);
      await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Close" })));

      await fireEvent.keyDown(window, { key: "Escape" });
      expect(document.activeElement).toBe(openButton);
    });

    it("traps Tab so focus wraps from the last to the first focusable element", async () => {
      render(BottomSheetFocusHarness);
      await fireEvent.click(screen.getByRole("button", { name: "Open" }));
      await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Close" })));

      const sheet = screen.getByRole("dialog");
      const close = screen.getByRole("button", { name: "Close" });
      const second = screen.getByRole("button", { name: "Second" });
      second.focus();

      await fireEvent.keyDown(sheet, { key: "Tab" });
      expect(document.activeElement).toBe(close);
    });

    it("traps Shift+Tab so focus wraps from the first to the last focusable element", async () => {
      render(BottomSheetFocusHarness);
      await fireEvent.click(screen.getByRole("button", { name: "Open" }));
      await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Close" })));

      const sheet = screen.getByRole("dialog");
      await fireEvent.keyDown(sheet, { key: "Tab", shiftKey: true });
      expect(document.activeElement).toBe(screen.getByRole("button", { name: "Second" }));
    });
  });
});
