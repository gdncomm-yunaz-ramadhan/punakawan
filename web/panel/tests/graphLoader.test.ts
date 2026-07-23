import { beforeEach, describe, expect, it, vi } from "vitest";

const factorySpy = vi.fn();

vi.mock("cytoscape", () => {
  return { default: factorySpy };
});

beforeEach(() => {
  factorySpy.mockClear();
});

describe("graphLoader", () => {
  it("returns the same cytoscape module across repeated loadCytoscape calls (singleton)", async () => {
    const { loadCytoscape, __resetGraphLoaderForTests } = await import(
      "../src/lib/components/graphs/graphLoader"
    );
    __resetGraphLoaderForTests();

    const first = await loadCytoscape();
    const second = await loadCytoscape();

    expect(first).toBe(second);
    expect(first).toBe(factorySpy);
  });

  it("caches the in-flight promise so concurrent callers share one dynamic import", async () => {
    const { loadCytoscape, __resetGraphLoaderForTests } = await import(
      "../src/lib/components/graphs/graphLoader"
    );
    __resetGraphLoaderForTests();

    const [a, b] = await Promise.all([loadCytoscape(), loadCytoscape()]);
    expect(a).toBe(b);
  });
});
