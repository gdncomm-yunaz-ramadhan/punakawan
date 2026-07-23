import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it, vi } from "vitest";
import AddCommentPopover from "../src/lib/components/review/AddCommentPopover.svelte";

describe("AddCommentPopover", () => {
  it("shows the anchor context: heading path and quoted text", () => {
    render(AddCommentPopover, {
      props: {
        headingPath: ["Plan", "Section"],
        quotedText: "some quoted text",
        onsubmit: () => {},
        oncancel: () => {},
      },
    });

    expect(screen.getByText("Plan › Section")).toBeTruthy();
    expect(screen.getByText(/some quoted text/)).toBeTruthy();
  });

  it("disables submit and shows a validation error when the body is empty/whitespace", async () => {
    const onsubmit = vi.fn();
    render(AddCommentPopover, { props: { headingPath: ["Plan"], onsubmit, oncancel: () => {} } });

    const submit = screen.getByRole("button", { name: "Add Comment" }) as HTMLButtonElement;
    expect(submit.disabled).toBe(true);

    await fireEvent.input(screen.getByTestId("comment-body-input"), { target: { value: "   " } });
    expect(submit.disabled).toBe(true);
    expect(screen.getByRole("alert").textContent).toContain("cannot be empty");
    expect(onsubmit).not.toHaveBeenCalled();
  });

  it("calls onsubmit with the trimmed body when valid", async () => {
    const onsubmit = vi.fn();
    render(AddCommentPopover, { props: { headingPath: ["Plan"], onsubmit, oncancel: () => {} } });

    await fireEvent.input(screen.getByTestId("comment-body-input"), { target: { value: "  hello world  " } });
    await fireEvent.click(screen.getByRole("button", { name: "Add Comment" }));

    expect(onsubmit).toHaveBeenCalledWith("hello world");
  });

  it("calls oncancel when Cancel is clicked", async () => {
    const oncancel = vi.fn();
    render(AddCommentPopover, { props: { headingPath: ["Plan"], onsubmit: () => {}, oncancel } });

    await fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(oncancel).toHaveBeenCalled();
  });

  it("disables actions and shows Saving… while submitting", () => {
    render(AddCommentPopover, {
      props: { headingPath: ["Plan"], submitting: true, onsubmit: () => {}, oncancel: () => {} },
    });

    expect((screen.getByRole("button", { name: "Saving…" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("notifies ondraftchange on every keystroke", async () => {
    const ondraftchange = vi.fn();
    render(AddCommentPopover, {
      props: { headingPath: ["Plan"], onsubmit: () => {}, oncancel: () => {}, ondraftchange },
    });

    await fireEvent.input(screen.getByTestId("comment-body-input"), { target: { value: "a" } });
    expect(ondraftchange).toHaveBeenCalledWith("a");
  });
});
