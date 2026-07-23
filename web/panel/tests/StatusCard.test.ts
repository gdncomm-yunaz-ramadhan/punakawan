import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import StatusCard from "../src/lib/components/cards/StatusCard.svelte";

describe("StatusCard", () => {
  it("renders without crashing and shows the label", () => {
    render(StatusCard, { props: { variant: "success", label: "All clear" } });
    expect(screen.getByText("All clear")).toBeTruthy();
  });

  it("shows the optional description", () => {
    render(StatusCard, {
      props: { variant: "warning", label: "Needs review", description: "3 revisions pending" },
    });
    expect(screen.getByText("3 revisions pending")).toBeTruthy();
  });

  it("always pairs the semantic color with a visible icon glyph, never color alone", () => {
    render(StatusCard, { props: { variant: "danger", label: "Failed" } });
    const icon = document.querySelector(".icon");
    expect(icon).toBeTruthy();
    expect(icon?.getAttribute("aria-hidden")).toBe("true");
    expect(icon?.textContent).not.toBe("");
  });

  it.each(["success", "warning", "danger", "info"] as const)("renders the %s variant class", (variant) => {
    render(StatusCard, { props: { variant, label: "Status" } });
    expect(document.querySelector(`.status-${variant}`)).toBeTruthy();
  });
});
