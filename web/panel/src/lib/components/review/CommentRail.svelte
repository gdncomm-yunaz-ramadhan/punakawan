<script lang="ts">
  import CommentThread from "./CommentThread.svelte";
  import type { ArtifactComment } from "../../review/api";

  interface Props {
    comments: ArtifactComment[];
    // Full ordered list of anchor position keys (joined heading paths for
    // a plan, field_paths for a recipe) as they appear in the document, so
    // comments can be grouped/ordered by position rather than creation
    // order (§13.10 "grouped/ordered by their position in the document if
    // practical").
    documentHeadingOrder: string[];
    editable: boolean;
    busyCommentId?: string | null;
    onEditComment: (commentId: string, body: string) => void;
    onDeleteComment: (commentId: string) => void;
    showObsolete?: boolean;
  }
  let {
    comments,
    documentHeadingOrder,
    editable,
    busyCommentId = null,
    onEditComment,
    onDeleteComment,
    showObsolete = true,
  }: Props = $props();

  function orderKey(comment: ArtifactComment): number {
    const path = comment.anchor.field_path ?? (comment.anchor.heading_path ?? []).join(" › ");
    const idx = documentHeadingOrder.indexOf(path);
    return idx === -1 ? documentHeadingOrder.length : idx;
  }

  const visibleComments = $derived(
    (showObsolete ? comments : comments.filter((c) => c.status !== "obsolete")).slice().sort((a, b) => {
      const keyDiff = orderKey(a) - orderKey(b);
      if (keyDiff !== 0) return keyDiff;
      return a.id.localeCompare(b.id);
    }),
  );
</script>

<div class="rail" data-testid="comment-rail">
  {#if visibleComments.length === 0}
    <p class="empty">No comments yet. Click a section or select text in the document to add one.</p>
  {:else}
    {#each visibleComments as comment (comment.id)}
      <CommentThread
        {comment}
        editable={editable && comment.status !== "obsolete"}
        busy={busyCommentId === comment.id}
        onedit={(body) => onEditComment(comment.id, body)}
        ondelete={() => onDeleteComment(comment.id)}
      />
    {/each}
  {/if}
</div>

<style>
  .rail {
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
  }
  .empty {
    color: var(--color-text-muted);
    font-size: 0.85rem;
    text-align: center;
    padding: 1.5rem 0.5rem;
  }
</style>
