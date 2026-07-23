import { fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import DialogHarness from "./fixtures/DialogHarness.svelte";
import DialogFocusHarness from "./fixtures/DialogFocusHarness.svelte";

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

  describe("focus management (WCAG 2.4.3, §13.13)", () => {
    // DOM order inside the dialog is Close (in .dialog-head), then the
    // caller's own content (First, Second) - so Close is the first
    // focusable element and Second is the last.
    it("moves focus into the dialog on open", async () => {
      render(DialogFocusHarness);
      await fireEvent.click(screen.getByRole("button", { name: "Open" }));

      await waitFor(() => {
        expect(document.activeElement).toBe(screen.getByRole("button", { name: "Close" }));
      });
    });

    it("restores focus to the trigger after closing", async () => {
      render(DialogFocusHarness);
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
      render(DialogFocusHarness);
      const openButton = screen.getByRole("button", { name: "Open" });
      openButton.focus();
      await fireEvent.click(openButton);
      await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Close" })));

      await fireEvent.keyDown(window, { key: "Escape" });
      expect(document.activeElement).toBe(openButton);
    });

    it("traps Tab so focus wraps from the last to the first focusable element", async () => {
      render(DialogFocusHarness);
      await fireEvent.click(screen.getByRole("button", { name: "Open" }));
      await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Close" })));

      const dialog = screen.getByRole("dialog");
      const close = screen.getByRole("button", { name: "Close" });
      const second = screen.getByRole("button", { name: "Second" });
      second.focus();
      expect(document.activeElement).toBe(second);

      // Tab from the last focusable element (Second) wraps to the first (Close).
      await fireEvent.keyDown(dialog, { key: "Tab" });
      expect(document.activeElement).toBe(close);
    });

    it("traps Shift+Tab so focus wraps from the first to the last focusable element", async () => {
      render(DialogFocusHarness);
      await fireEvent.click(screen.getByRole("button", { name: "Open" }));
      await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Close" })));

      const dialog = screen.getByRole("dialog");
      await fireEvent.keyDown(dialog, { key: "Tab", shiftKey: true });
      expect(document.activeElement).toBe(screen.getByRole("button", { name: "Second" }));
    });
  });
});
