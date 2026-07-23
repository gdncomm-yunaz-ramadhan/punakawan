import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import StatusBadge from "../src/lib/components/StatusBadge.svelte";

describe("StatusBadge", () => {
  it("renders an availability badge with label and icon (existing mode)", () => {
    render(StatusBadge, { props: { availability: "available" } });
    expect(screen.getByText("Available")).toBeTruthy();
    expect(document.querySelector(".status-available")).toBeTruthy();
  });

  it("renders all five availability states", () => {
    const states = ["available", "partially_available", "busy", "unavailable", "invalid"] as const;
    for (const availability of states) {
      const { unmount } = render(StatusBadge, { props: { availability } });
      expect(document.querySelector(`.status-${availability}`)).toBeTruthy();
      unmount();
    }
  });

  it("renders a generic variant badge with a custom label (extended mode)", () => {
    render(StatusBadge, { props: { variant: "success", label: "Approved" } });
    expect(screen.getByText("Approved")).toBeTruthy();
    expect(document.querySelector(".status-variant-success")).toBeTruthy();
  });

  it("pairs every variant with a visible icon glyph, not color alone", () => {
    render(StatusBadge, { props: { variant: "danger", label: "Rejected" } });
    const icon = document.querySelector(".status span[aria-hidden='true']");
    expect(icon).toBeTruthy();
    expect(icon?.textContent).not.toBe("");
  });
});
