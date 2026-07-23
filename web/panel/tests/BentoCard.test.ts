import { render, screen } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import BentoCardHarness from "./fixtures/BentoCardHarness.svelte";

function getBentoCardEl(): HTMLElement {
  return document.querySelector(".bento-card") as HTMLElement;
}

describe("BentoCard", () => {
  it("renders without crashing", () => {
    render(BentoCardHarness);
    expect(screen.getByTestId("bento-card-content")).toBeTruthy();
  });

  it("defaults to a 4-column span for size='medium'", () => {
    render(BentoCardHarness, { props: { size: "medium" } });
    const el = getBentoCardEl();
    expect(el.dataset.columns).toBe("4");
    expect(el.style.gridColumn).toContain("span 4");
    expect(el.dataset.rows).toBe("1");
  });

  it("uses a 3-column span for size='small'", () => {
    render(BentoCardHarness, { props: { size: "small" } });
    const el = getBentoCardEl();
    expect(el.dataset.columns).toBe("3");
    expect(el.style.gridColumn).toContain("span 3");
  });

  it("defaults to an 8-column span for size='wide'", () => {
    render(BentoCardHarness, { props: { size: "wide" } });
    const el = getBentoCardEl();
    expect(el.dataset.columns).toBe("8");
    expect(el.style.gridColumn).toContain("span 8");
  });

  it("spans a full 12 columns for size='full'", () => {
    render(BentoCardHarness, { props: { size: "full" } });
    const el = getBentoCardEl();
    expect(el.dataset.columns).toBe("12");
  });

  it("spans 2 rows for size='tall' while keeping its own column span", () => {
    render(BentoCardHarness, { props: { size: "tall", columns: 6 } });
    const el = getBentoCardEl();
    expect(el.dataset.rows).toBe("2");
    expect(el.style.gridRow).toContain("span 2");
    expect(el.dataset.columns).toBe("6");
  });

  it("respects an explicit columns override", () => {
    render(BentoCardHarness, { props: { size: "medium", columns: 5 } });
    const el = getBentoCardEl();
    expect(el.dataset.columns).toBe("5");
    expect(el.style.gridColumn).toContain("span 5");
  });
});
