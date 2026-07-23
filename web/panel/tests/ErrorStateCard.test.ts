import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import ErrorStateCard from "../src/lib/components/cards/ErrorStateCard.svelte";

describe("ErrorStateCard", () => {
  it("renders without crashing with default copy as an alert", () => {
    render(ErrorStateCard);
    expect(screen.getByRole("alert").textContent).toContain("Something went wrong");
  });

  it("renders a custom title and message", () => {
    render(ErrorStateCard, { props: { title: "Load failed", message: "Could not reach the server." } });
    expect(screen.getByText("Load failed")).toBeTruthy();
    expect(screen.getByText("Could not reach the server.")).toBeTruthy();
  });
});
