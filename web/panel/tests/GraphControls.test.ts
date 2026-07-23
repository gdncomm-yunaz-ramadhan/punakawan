import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it, vi } from "vitest";
import GraphControls from "../src/lib/components/graphs/GraphControls.svelte";
import type { GraphCanvasApi } from "../src/lib/components/graphs/GraphCanvas.svelte";

function fakeApi(): GraphCanvasApi {
  return { zoomIn: vi.fn(), zoomOut: vi.fn(), fit: vi.fn(), runLayout: vi.fn() };
}

describe("GraphControls", () => {
  it("renders zoom, fit, and layout controls", () => {
    render(GraphControls, {
      props: { api: fakeApi(), layoutName: "cose", onLayoutChange: vi.fn() },
    });
    expect(screen.getByRole("button", { name: "Zoom in" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Zoom out" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Fit to view" })).toBeTruthy();
    expect(screen.getByRole("combobox", { name: "Layout" })).toBeTruthy();
  });

  it("offers at least the cose and breadthfirst layouts", () => {
    render(GraphControls, {
      props: { api: fakeApi(), layoutName: "cose", onLayoutChange: vi.fn() },
    });
    const select = screen.getByRole("combobox", { name: "Layout" }) as HTMLSelectElement;
    const optionValues = Array.from(select.options).map((o) => o.value);
    expect(optionValues).toContain("cose");
    expect(optionValues).toContain("breadthfirst");
  });

  it("calls the api's zoomIn/zoomOut/fit on click", async () => {
    const api = fakeApi();
    render(GraphControls, { props: { api, layoutName: "cose", onLayoutChange: vi.fn() } });

    await fireEvent.click(screen.getByRole("button", { name: "Zoom in" }));
    expect(api.zoomIn).toHaveBeenCalledOnce();

    await fireEvent.click(screen.getByRole("button", { name: "Zoom out" }));
    expect(api.zoomOut).toHaveBeenCalledOnce();

    await fireEvent.click(screen.getByRole("button", { name: "Fit to view" }));
    expect(api.fit).toHaveBeenCalledOnce();
  });

  it("calls onLayoutChange when the layout select changes", async () => {
    const onLayoutChange = vi.fn();
    render(GraphControls, { props: { api: fakeApi(), layoutName: "cose", onLayoutChange } });

    await fireEvent.change(screen.getByRole("combobox", { name: "Layout" }), {
      target: { value: "breadthfirst" },
    });
    expect(onLayoutChange).toHaveBeenCalledWith("breadthfirst");
  });

  it("disables the Expand action until a node is selected", async () => {
    const onExpand = vi.fn();
    render(GraphControls, {
      props: { api: fakeApi(), layoutName: "cose", onLayoutChange: vi.fn(), selectedNodeId: null, onExpand },
    });
    const expandButton = screen.getByRole("button", { name: "Expand selected node" }) as HTMLButtonElement;
    expect(expandButton.disabled).toBe(true);
  });

  it("enables and triggers the Expand action once a node is selected", async () => {
    const onExpand = vi.fn();
    render(GraphControls, {
      props: {
        api: fakeApi(),
        layoutName: "cose",
        onLayoutChange: vi.fn(),
        selectedNodeId: "node-1",
        onExpand,
      },
    });
    const expandButton = screen.getByRole("button", { name: "Expand selected node" }) as HTMLButtonElement;
    expect(expandButton.disabled).toBe(false);
    await fireEvent.click(expandButton);
    expect(onExpand).toHaveBeenCalledWith("node-1");
  });
});
