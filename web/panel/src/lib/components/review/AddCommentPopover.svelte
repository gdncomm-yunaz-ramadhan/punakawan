<script lang="ts">
  interface Props {
    // Markdown anchor context (plan review). Leave both undefined for a
    // recipe's field_path context instead - the two are mutually
    // exclusive per artifact type, never combined.
    headingPath?: string[];
    quotedText?: string;
    // recipe_field_path anchor context (retrieval_recipe review):
    // RecipeDocument's clicked field path and a preview of its value,
    // shown in place of the markdown heading-path/quoted-text context.
    fieldPath?: string;
    fieldPreview?: string;
    submitting?: boolean;
    onsubmit: (body: string) => void;
    oncancel: () => void;
    // Fires on every keystroke so a parent can track an unsaved draft
    // indicator (§13.10 "unsaved-change indicator").
    ondraftchange?: (body: string) => void;
  }
  let {
    headingPath = [],
    quotedText,
    fieldPath,
    fieldPreview,
    submitting = false,
    onsubmit,
    oncancel,
    ondraftchange,
  }: Props = $props();

  let body = $state("");

  const trimmedBody = $derived(body.trim());
  const canSubmit = $derived(trimmedBody.length > 0 && !submitting);

  function handleInput(e: Event) {
    body = (e.target as HTMLTextAreaElement).value;
    ondraftchange?.(body);
  }

  function submit() {
    if (!canSubmit) return;
    onsubmit(trimmedBody);
  }
</script>

<div class="popover" data-testid="add-comment-popover">
  <div class="anchor-context">
    {#if fieldPath}
      <span class="heading-path">{fieldPath}</span>
      {#if fieldPreview}
        <blockquote class="quoted">{fieldPreview}</blockquote>
      {/if}
    {:else}
      {#if headingPath.length > 0}
        <span class="heading-path">{headingPath.join(" › ")}</span>
      {/if}
      {#if quotedText}
        <blockquote class="quoted">&ldquo;{quotedText}&rdquo;</blockquote>
      {/if}
    {/if}
  </div>
  <textarea
    class="body-input"
    placeholder="Add your comment…"
    aria-label="Comment body"
    value={body}
    oninput={handleInput}
    data-testid="comment-body-input"
  ></textarea>
  {#if body.length > 0 && trimmedBody.length === 0}
    <p class="validation-error" role="alert">Comment cannot be empty.</p>
  {/if}
  <div class="actions">
    <button type="button" class="cancel" onclick={oncancel} disabled={submitting}>Cancel</button>
    <button type="button" class="submit" onclick={submit} disabled={!canSubmit}>
      {submitting ? "Saving…" : "Add Comment"}
    </button>
  </div>
</div>

<style>
  .popover {
    background: var(--color-surface-raised);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 0.75rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .anchor-context {
    display: flex;
    flex-direction: column;
    gap: 0.3rem;
  }
  .heading-path {
    font-size: 0.75rem;
    color: var(--color-text-muted);
    font-weight: 600;
  }
  .quoted {
    margin: 0;
    padding-left: 0.5rem;
    border-left: 2px solid var(--color-border-strong);
    font-size: 0.8rem;
    color: var(--color-text-muted);
    font-style: italic;
  }
  .body-input {
    width: 100%;
    min-height: 5rem;
    box-sizing: border-box;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: 0.5rem;
    font-family: inherit;
    font-size: 0.85rem;
    color: var(--color-text);
    background: var(--color-surface);
    resize: vertical;
  }
  .validation-error {
    margin: 0;
    color: var(--color-danger);
    font-size: 0.8rem;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
  }
  .cancel,
  .submit {
    border-radius: 6px;
    padding: 0.4rem 0.75rem;
    font-size: 0.85rem;
    cursor: pointer;
    min-height: 44px;
  }
  .cancel {
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    color: var(--color-text);
  }
  .submit {
    border: none;
    background: var(--color-accent);
    color: var(--color-accent-contrast);
  }
  .submit:disabled,
  .cancel:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
