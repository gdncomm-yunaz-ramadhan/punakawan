import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it, vi } from "vitest";
import ResponsiveToolbar, { type ToolbarAction } from "../src/lib/components/ResponsiveToolbar.svelte";

function actions(count: number, onSelect: (id: string) => void): ToolbarAction[] {
  return Array.from({ length: count }, (_, i) => ({
    id: `action-${i}`,
    label: `Action ${i}`,
    onSelect: () => onSelect(`action-${i}`),
  }));
}

describe("ResponsiveToolbar", () => {
  it("renders all actions inline with no overflow trigger when under visibleCount", () => {
    render(ResponsiveToolbar, { props: { actions: actions(2, () => {}), visibleCount: 3 } });

    expect(screen.getByRole("button", { name: "Action 0" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Action 1" })).toBeTruthy();
    expect(screen.queryByTestId("toolbar-overflow-trigger")).toBeNull();
  });

  it("shows an overflow trigger when actions exceed visibleCount", () => {
    render(ResponsiveToolbar, { props: { actions: actions(5, () => {}), visibleCount: 2 } });

    expect(screen.getByTestId("toolbar-overflow-trigger")).toBeTruthy();
    // Only the first two are visible outside the menu.
    expect(screen.getByRole("button", { name: "Action 0" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Action 1" })).toBeTruthy();
    expect(screen.queryByRole("menuitem", { name: "Action 2" })).toBeNull();
  });

  it("opens the overflow menu on click and invokes the selected action's callback", async () => {
    const onSelect = vi.fn();
    render(ResponsiveToolbar, { props: { actions: actions(5, onSelect), visibleCount: 2 } });

    await fireEvent.click(screen.getByTestId("toolbar-overflow-trigger"));
    const menuItem = screen.getByRole("menuitem", { name: "Action 3" });
    expect(menuItem).toBeTruthy();

    await fireEvent.click(menuItem);
    expect(onSelect).toHaveBeenCalledWith("action-3");
  });

  it("closes the overflow menu on Escape", async () => {
    render(ResponsiveToolbar, { props: { actions: actions(5, () => {}), visibleCount: 2 } });

    await fireEvent.click(screen.getByTestId("toolbar-overflow-trigger"));
    expect(screen.getByRole("menu")).toBeTruthy();

    await fireEvent.keyDown(screen.getByRole("toolbar"), { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
  });
});
