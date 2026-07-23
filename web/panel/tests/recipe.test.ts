import { describe, expect, it } from "vitest";
import { allFieldPaths, buildFieldPathAnchor, joinFieldPath } from "../src/lib/review/recipe";

describe("joinFieldPath", () => {
  it("joins object keys and array indices with '.', matching gjson syntax", () => {
    expect(joinFieldPath("retrieval_recipe", "selector", "all", 0, "value", "literal")).toBe(
      "retrieval_recipe.selector.all.0.value.literal",
    );
  });
});

describe("buildFieldPathAnchor", () => {
  it("builds the exact protocol.ArtifactCommentAnchor shape for recipe_field_path", () => {
    const anchor = buildFieldPathAnchor({
      baseRevisionHash: "sha256:abc",
      fieldPath: "retrieval_recipe.capability",
    });
    expect(anchor).toEqual({
      kind: "recipe_field_path",
      base_revision_hash: "sha256:abc",
      field_path: "retrieval_recipe.capability",
    });
  });
});

describe("allFieldPaths", () => {
  it("walks JSON content depth-first, returning every node's field_path in document order", () => {
    const content = JSON.stringify({
      id: "pkw:recipe/x",
      retrieval_recipe: {
        capability: "jira.issue.search",
        selector: { all: [{ field: "project", operator: "equals" }] },
      },
    });
    const paths = allFieldPaths(content);

    expect(paths).toContain("id");
    expect(paths).toContain("retrieval_recipe");
    expect(paths).toContain("retrieval_recipe.capability");
    expect(paths).toContain("retrieval_recipe.selector");
    expect(paths).toContain("retrieval_recipe.selector.all");
    expect(paths).toContain("retrieval_recipe.selector.all.0");
    expect(paths).toContain("retrieval_recipe.selector.all.0.field");
    expect(paths).toContain("retrieval_recipe.selector.all.0.operator");

    // Depth-first order: a parent path is always emitted before its children.
    expect(paths.indexOf("retrieval_recipe")).toBeLessThan(paths.indexOf("retrieval_recipe.capability"));
    expect(paths.indexOf("retrieval_recipe.selector.all")).toBeLessThan(
      paths.indexOf("retrieval_recipe.selector.all.0"),
    );
  });

  it("returns an empty list for invalid JSON rather than throwing", () => {
    expect(allFieldPaths("not json")).toEqual([]);
  });
});
