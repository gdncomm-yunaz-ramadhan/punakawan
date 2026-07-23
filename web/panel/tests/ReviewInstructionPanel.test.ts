import { fireEvent, render, screen } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ReviewInstructionPanel from "../src/lib/components/review/ReviewInstructionPanel.svelte";

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("ReviewInstructionPanel", () => {
  it("shows Saved initially with no pending change", () => {
    render(ReviewInstructionPanel, { props: { instruction: "initial text", onsave: async () => {} } });
    expect(screen.getByTestId("instruction-status").textContent).toBe("Saved");
  });

  it("shows Unsaved changes immediately after typing, before the debounce fires", async () => {
    render(ReviewInstructionPanel, {
      props: { instruction: "initial", onsave: async () => {}, debounceMs: 500 },
    });

    await fireEvent.input(screen.getByTestId("instruction-input"), { target: { value: "initial + edit" } });
    expect(screen.getByTestId("instruction-status").textContent).toBe("Unsaved changes");
  });

  it("calls onsave after the debounce interval and shows Saved again", async () => {
    const onsave = vi.fn().mockResolvedValue(undefined);
    render(ReviewInstructionPanel, { props: { instruction: "initial", onsave, debounceMs: 500 } });

    await fireEvent.input(screen.getByTestId("instruction-input"), { target: { value: "updated" } });
    expect(onsave).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(500);

    expect(onsave).toHaveBeenCalledWith("updated");
    expect(screen.getByTestId("instruction-status").textContent).toBe("Saved");
  });

  it("debounces rapid keystrokes into a single save call", async () => {
    const onsave = vi.fn().mockResolvedValue(undefined);
    render(ReviewInstructionPanel, { props: { instruction: "", onsave, debounceMs: 500 } });

    const input = screen.getByTestId("instruction-input");
    await fireEvent.input(input, { target: { value: "a" } });
    await vi.advanceTimersByTimeAsync(200);
    await fireEvent.input(input, { target: { value: "ab" } });
    await vi.advanceTimersByTimeAsync(200);
    await fireEvent.input(input, { target: { value: "abc" } });
    await vi.advanceTimersByTimeAsync(500);

    expect(onsave).toHaveBeenCalledTimes(1);
    expect(onsave).toHaveBeenCalledWith("abc");
  });

  it("shows a save error and keeps the unsaved indicator when onsave rejects", async () => {
    const onsave = vi.fn().mockRejectedValue(new Error("network down"));
    render(ReviewInstructionPanel, { props: { instruction: "initial", onsave, debounceMs: 100 } });

    await fireEvent.input(screen.getByTestId("instruction-input"), { target: { value: "updated" } });
    await vi.advanceTimersByTimeAsync(100);

    expect(screen.getByRole("alert").textContent).toContain("network down");
    expect(screen.getByTestId("instruction-status").textContent).toBe("Unsaved changes");
  });
});
