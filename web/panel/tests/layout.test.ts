import { describe, expect, it } from "vitest";
import { externalRefs, layoutGraph } from "../src/lib/graph/layout";

describe("layoutGraph", () => {
  it("levels a linear chain by dependency depth", () => {
    // a depends on b depends on c: c has no deps (level 0), b depends on
    // c (level 1), a depends on b (level 2).
    const { levels, maxLevel } = layoutGraph(
      ["a", "b", "c"],
      [
        { From: "a", To: "b" },
        { From: "b", To: "c" },
      ],
    );
    expect(levels.get("c")).toBe(0);
    expect(levels.get("b")).toBe(1);
    expect(levels.get("a")).toBe(2);
    expect(maxLevel).toBe(2);
  });

  it("does not infinite-loop on a cycle", () => {
    const { levels } = layoutGraph(
      ["a", "b"],
      [
        { From: "a", To: "b" },
        { From: "b", To: "a" },
      ],
    );
    expect(levels.size).toBe(2);
  });

  it("counts an external reference as one level without a matching node", () => {
    const { levels } = layoutGraph(["a"], [{ From: "a", To: "external:other-project:cap" }]);
    expect(levels.get("a")).toBe(1);
  });
});

describe("externalRefs", () => {
  it("lists dependency targets with no matching node", () => {
    const refs = externalRefs(
      ["a", "b"],
      [
        { From: "a", To: "b" },
        { From: "a", To: "external:proj:cap" },
      ],
    );
    expect(refs).toEqual(["external:proj:cap"]);
  });

  it("returns nothing when every target is a known node", () => {
    expect(externalRefs(["a", "b"], [{ From: "a", To: "b" }])).toEqual([]);
  });
});
