import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it, vi } from "vitest";
import Pagination from "../src/lib/components/data/Pagination.svelte";

describe("Pagination", () => {
  it("renders without crashing and shows current/total pages", () => {
    render(Pagination, { props: { currentPage: 2, totalPages: 5, pageSize: 10, onPageChange: vi.fn() } });
    expect(screen.getByText("Page 2 of 5")).toBeTruthy();
  });

  it("emits the next page number when Next is clicked", async () => {
    const onPageChange = vi.fn();
    render(Pagination, { props: { currentPage: 2, totalPages: 5, pageSize: 10, onPageChange } });
    await fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(onPageChange).toHaveBeenCalledWith(3);
  });

  it("emits the previous page number when Previous is clicked", async () => {
    const onPageChange = vi.fn();
    render(Pagination, { props: { currentPage: 3, totalPages: 5, pageSize: 10, onPageChange } });
    await fireEvent.click(screen.getByRole("button", { name: "Previous" }));
    expect(onPageChange).toHaveBeenCalledWith(2);
  });

  it("disables Previous on the first page and Next on the last page", () => {
    render(Pagination, { props: { currentPage: 1, totalPages: 1, pageSize: 10, onPageChange: vi.fn() } });
    expect((screen.getByRole("button", { name: "Previous" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Next" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("emits a page size change when the selector changes", async () => {
    const onPageSizeChange = vi.fn();
    render(Pagination, {
      props: { currentPage: 1, totalPages: 3, pageSize: 10, onPageChange: vi.fn(), onPageSizeChange },
    });
    await fireEvent.change(screen.getByRole("combobox"), { target: { value: "25" } });
    expect(onPageSizeChange).toHaveBeenCalledWith(25);
  });

  it("does not render a page-size selector when onPageSizeChange is not provided", () => {
    render(Pagination, { props: { currentPage: 1, totalPages: 3, pageSize: 10, onPageChange: vi.fn() } });
    expect(screen.queryByRole("combobox")).toBeNull();
  });
});
