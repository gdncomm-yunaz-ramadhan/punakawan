<script lang="ts">
  import StatusBadge, { type BadgeVariant } from "../StatusBadge.svelte";
  import type { ArtifactComment, CommentStatus } from "../../review/api";

  interface Props {
    comment: ArtifactComment;
    editable: boolean;
    busy?: boolean;
    onedit: (body: string) => void;
    ondelete: () => void;
  }
  let { comment, editable, busy = false, onedit, ondelete }: Props = $props();

  let editing = $state(false);
  // Set for real when editing starts (startEdit), not here - reading
  // `comment.body` at declaration time would only capture the prop's
  // initial value rather than staying in sync with later prop updates.
  let draft = $state("");

  const statusVariant: Record<CommentStatus, BadgeVariant> = {
    open: "info",
    addressed: "success",
    partially_addressed: "warning",
    rejected_by_agent: "danger",
    needs_clarification: "warning",
    resolved_by_user: "success",
    obsolete: "neutral",
  };
  const statusLabel: Record<CommentStatus, string> = {
    open: "Open",
    addressed: "Addressed",
    partially_addressed: "Partially addressed",
    rejected_by_agent: "Rejected by agent",
    needs_clarification: "Needs clarification",
    resolved_by_user: "Resolved",
    obsolete: "Deleted",
  };

  function startEdit() {
    draft = comment.body;
    editing = true;
  }
  function cancelEdit() {
    editing = false;
  }
  function saveEdit() {
    const trimmed = draft.trim();
    if (!trimmed) return;
    onedit(trimmed);
    editing = false;
  }

  const isObsolete = $derived(comment.status === "obsolete");
</script>

<div class="thread" data-testid="comment-thread" class:obsolete={isObsolete}>
  <div class="thread-head">
    <span class="heading-path">
      {#if comment.anchor.heading_path?.length}
        {comment.anchor.heading_path.join(" › ")}
      {:else if comment.anchor.field_path}
        {comment.anchor.field_path}
      {:else}
        (unanchored)
      {/if}
    </span>
    <StatusBadge variant={statusVariant[comment.status]} label={statusLabel[comment.status]} />
  </div>
  {#if comment.anchor.quoted_text}
    <blockquote class="quoted">&ldquo;{comment.anchor.quoted_text}&rdquo;</blockquote>
  {/if}

  {#if editing}
    <textarea
      class="edit-input"
      aria-label="Edit comment"
      bind:value={draft}
      data-testid="comment-edit-input"
    ></textarea>
    <div class="actions">
      <button type="button" onclick={cancelEdit} disabled={busy}>Cancel</button>
      <button type="button" onclick={saveEdit} disabled={busy || !draft.trim()}>Save</button>
    </div>
  {:else}
    <p class="body">{comment.body}</p>
    <div class="meta">
      <span class="author">{comment.author}</span>
      {#if editable && !isObsolete}
        <div class="actions">
          <button type="button" onclick={startEdit} disabled={busy}>Edit</button>
          <button type="button" class="delete" onclick={ondelete} disabled={busy}>Delete</button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .thread {
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    background: var(--color-surface);
    padding: 0.65rem 0.75rem;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }
  .thread.obsolete {
    opacity: 0.55;
  }
  .thread-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
  }
  .heading-path {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--color-text-muted);
  }
  .quoted {
    margin: 0;
    padding-left: 0.5rem;
    border-left: 2px solid var(--color-border-strong);
    font-size: 0.78rem;
    color: var(--color-text-muted);
    font-style: italic;
  }
  .body {
    margin: 0;
    color: var(--color-text);
    font-size: 0.88rem;
    white-space: pre-wrap;
  }
  .meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
  }
  .author {
    font-size: 0.72rem;
    color: var(--color-text-muted);
  }
  .actions {
    display: flex;
    gap: 0.4rem;
    justify-content: flex-end;
  }
  .actions button {
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    color: var(--color-text);
    border-radius: 6px;
    padding: 0.25rem 0.55rem;
    font-size: 0.78rem;
    cursor: pointer;
    min-height: 44px;
    min-width: 44px;
  }
  .actions button.delete {
    color: var(--color-danger);
  }
  .actions button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .edit-input {
    width: 100%;
    box-sizing: border-box;
    min-height: 4rem;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: 0.4rem;
    font-family: inherit;
    font-size: 0.85rem;
    color: var(--color-text);
    background: var(--color-surface);
    resize: vertical;
  }
</style>
