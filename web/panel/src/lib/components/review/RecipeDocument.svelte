<script lang="ts">
  // Recipe review's equivalent of PlanDocument.svelte: renders a
  // retrieval_recipe artifact's canonical content (indented JSON of the
  // whole protocol.KnowledgeRecord, per internal/recipe.RecipeStore.
  // MarshalCanonical) as a clickable field tree, per punokawan-q9r.6's
  // instruction that a structured document needs a "click a field,
  // comment on it" interaction rather than PlanDocument's markdown-
  // specific section-hover/text-selection affordances, which don't apply
  // to JSON. Every leaf value and every array/object node gets its own
  // "Comment on this field" affordance, anchored by gjson-syntax
  // field_path (internal/artifact/recipefieldpath.go's single exact-match
  // resolver - no fuzzy fallback, so the path must be exact).
  import { joinFieldPath } from "../../review/recipe";

  export interface FieldCommentRequest {
    fieldPath: string;
    // A short preview of the value at fieldPath, shown in the comment
    // popover for context - not sent to the server (the anchor itself
    // carries no quoted_text for recipe_field_path, per the schema).
    preview: string;
  }

  interface Props {
    content: string;
    onCommentField: (req: FieldCommentRequest) => void;
  }
  let { content, onCommentField }: Props = $props();

  type JSONValue = string | number | boolean | null | JSONValue[] | { [key: string]: JSONValue };

  // A single derived tuple, not two separate derived reads of the same
  // try/catch - Svelte 5 forbids mutating a second piece of $state from
  // inside a $derived's own callback (state_unsafe_mutation).
  const parseResult = $derived.by<{ value: JSONValue | null; error: string | null }>(() => {
    try {
      return { value: JSON.parse(content) as JSONValue, error: null };
    } catch (e) {
      return { value: null, error: e instanceof Error ? e.message : String(e) };
    }
  });
  const parsed = $derived(parseResult.value);
  const parseError = $derived(parseResult.error);

  function previewOf(value: JSONValue): string {
    if (value === null) return "null";
    if (Array.isArray(value)) return `[${value.length} item${value.length === 1 ? "" : "s"}]`;
    if (typeof value === "object") {
      const keys = Object.keys(value);
      return `{${keys.length} field${keys.length === 1 ? "" : "s"}}`;
    }
    return JSON.stringify(value);
  }

  function isContainer(value: JSONValue): value is JSONValue[] | { [key: string]: JSONValue } {
    return value !== null && typeof value === "object";
  }
</script>

{#snippet node(value: JSONValue, path: string, label: string, depth: number)}
  <div class="node" style:--depth={depth} data-testid="recipe-field-node" data-field-path={path}>
    <div class="node-row">
      <span class="key">{label}</span>
      {#if !isContainer(value)}
        <span class="value">{previewOf(value)}</span>
      {:else}
        <span class="value muted">{previewOf(value)}</span>
      {/if}
      <button
        type="button"
        class="comment-affordance"
        data-testid="comment-field-button"
        onclick={() => onCommentField({ fieldPath: path, preview: previewOf(value) })}
      >
        + Comment
      </button>
    </div>
    {#if isContainer(value)}
      <div class="children">
        {#if Array.isArray(value)}
          {#each value as item, i (i)}
            {@render node(item, joinFieldPath(path, i), `[${i}]`, depth + 1)}
          {/each}
        {:else}
          {#each Object.entries(value) as [key, item] (key)}
            {@render node(item, joinFieldPath(path, key), key, depth + 1)}
          {/each}
        {/if}
      </div>
    {/if}
  </div>
{/snippet}

<div class="recipe-document" data-testid="recipe-document">
  {#if parseError}
    <p role="alert" class="error">This recipe's content is not valid JSON: {parseError}</p>
  {:else if parsed && isContainer(parsed)}
    <div class="root">
      {#if Array.isArray(parsed)}
        {#each parsed as item, i (i)}
          {@render node(item, String(i), `[${i}]`, 0)}
        {/each}
      {:else}
        {#each Object.entries(parsed) as [key, item] (key)}
          {@render node(item, key, key, 0)}
        {/each}
      {/if}
    </div>
  {/if}
</div>

<style>
  .recipe-document {
    color: var(--color-text);
    font-family: monospace;
    font-size: 0.82rem;
    line-height: 1.5;
  }
  .error {
    color: var(--color-danger);
    font-family: inherit;
  }
  .node {
    margin-left: calc(var(--depth, 0) * 0.9rem);
  }
  .node-row {
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
    flex-wrap: wrap;
    padding: 0.1rem 0;
  }
  .key {
    color: var(--color-accent);
    font-weight: 600;
  }
  .value {
    color: var(--color-text);
    word-break: break-word;
  }
  .value.muted {
    color: var(--color-text-muted);
    font-style: italic;
  }
  .comment-affordance {
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    color: var(--color-text-muted);
    border-radius: 6px;
    padding: 0.05rem 0.45rem;
    font-size: 0.7rem;
    font-family: system-ui, sans-serif;
    cursor: pointer;
    min-height: 28px;
  }
  @media (hover: hover) and (pointer: fine) {
    .comment-affordance {
      opacity: 0;
    }
    .node-row:hover .comment-affordance,
    .comment-affordance:focus-visible {
      opacity: 1;
    }
  }
  @media (hover: hover) and (pointer: fine) and (prefers-reduced-motion: no-preference) {
    .comment-affordance {
      transition: opacity 120ms ease;
    }
  }
  .children {
    border-left: 1px solid var(--color-border);
    padding-left: 0.4rem;
  }
</style>
