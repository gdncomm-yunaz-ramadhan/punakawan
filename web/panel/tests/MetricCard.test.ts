import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import MetricCard from "../src/lib/components/cards/MetricCard.svelte";

describe("MetricCard", () => {
  it("renders without crashing and shows the value and label", () => {
    render(MetricCard, { props: { label: "Active revisions", value: 12 } });
    expect(screen.getByText("12")).toBeTruthy();
    expect(screen.getByText("Active revisions")).toBeTruthy();
  });

  it("shows an up trend with its delta", () => {
    render(MetricCard, {
      props: { label: "Accepted this week", value: 8, trendDirection: "up", trendDelta: "+3" },
    });
    expect(screen.getByText("+3")).toBeTruthy();
  });

  it("shows a down trend with its delta", () => {
    render(MetricCard, {
      props: { label: "Failed runs", value: 2, trendDirection: "down", trendDelta: "-1" },
    });
    expect(screen.getByText("-1")).toBeTruthy();
  });

  it("omits the trend indicator when no trend is given", () => {
    render(MetricCard, { props: { label: "Reviews needing input", value: 4 } });
    expect(document.querySelector(".trend")).toBeNull();
  });
});
