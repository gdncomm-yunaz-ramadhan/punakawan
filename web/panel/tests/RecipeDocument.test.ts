import { fireEvent, render, screen } from "@testing-library/svelte";
import { describe, expect, it, vi } from "vitest";
import RecipeDocument from "../src/lib/components/review/RecipeDocument.svelte";

const sampleRecipeJson = JSON.stringify(
  {
    id: "pkw:recipe/affiliate-api/jira-next-sprint",
    retrieval_recipe: {
      capability: "jira.issue.search",
      selector: {
        all: [{ field: "project", operator: "equals", value: { literal: "AFF" } }],
      },
    },
  },
  null,
  2,
);

describe("RecipeDocument", () => {
  it("renders every JSON field as a clickable node", () => {
    render(RecipeDocument, { props: { content: sampleRecipeJson, onCommentField: () => {} } });

    const nodes = screen.getAllByTestId("recipe-field-node");
    const paths = nodes.map((n) => n.getAttribute("data-field-path"));
    expect(paths).toContain("id");
    expect(paths).toContain("retrieval_recipe");
    expect(paths).toContain("retrieval_recipe.capability");
    expect(paths).toContain("retrieval_recipe.selector.all.0.field");
  });

  it("clicking a field's comment affordance calls onCommentField with its exact field_path", async () => {
    const onCommentField = vi.fn();
    render(RecipeDocument, { props: { content: sampleRecipeJson, onCommentField } });

    const capabilityNode = screen
      .getAllByTestId("recipe-field-node")
      .find((n) => n.getAttribute("data-field-path") === "retrieval_recipe.capability");
    expect(capabilityNode).toBeTruthy();

    const button = capabilityNode!.querySelector('[data-testid="comment-field-button"]');
    expect(button).toBeTruthy();
    await fireEvent.click(button!);

    expect(onCommentField).toHaveBeenCalledWith(
      expect.objectContaining({ fieldPath: "retrieval_recipe.capability", preview: '"jira.issue.search"' }),
    );
  });

  it("shows an error instead of throwing when content is not valid JSON", () => {
    render(RecipeDocument, { props: { content: "not json", onCommentField: () => {} } });
    expect(screen.getByRole("alert")).toBeTruthy();
  });
});
