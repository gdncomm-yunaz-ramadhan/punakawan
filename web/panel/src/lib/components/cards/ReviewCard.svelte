<script lang="ts">
  import BentoCard, { type BentoSize } from "./BentoCard.svelte";
  import type { CardState } from "./Card.svelte";
  import StatusBadge, { type BadgeVariant } from "../StatusBadge.svelte";

  interface Props {
    title: string;
    statusVariant: BadgeVariant;
    statusLabel: string;
    artifactId: string;
    version: string | number;
    commentCount: number;
    size?: BentoSize;
    state?: CardState;
    emptyMessage?: string;
    onselect?: () => void;
  }
  let {
    title,
    statusVariant,
    statusLabel,
    artifactId,
    version,
    commentCount,
    size = "medium",
    state = "default",
    emptyMessage,
    onselect,
  }: Props = $props();
</script>

<!--
  Review title, status chip, artifact id/version, and comment count
  (UI-010). Reuses StatusBadge's generic variant mode rather than a
  bespoke chip.
-->
<BentoCard {size} {state} {emptyMessage}>
  {#snippet children()}
    <div class="review">
      <div class="review-head">
        {#if onselect}
          <button type="button" class="title-button" onclick={onselect}>{title}</button>
        {:else}
          <span class="title">{title}</span>
        {/if}
        <StatusBadge variant={statusVariant} label={statusLabel} />
      </div>
      <div class="meta">
        <span class="artifact">{artifactId} · v{version}</span>
        <span class="comments">
          <span aria-hidden="true">💬</span>
          {commentCount} comment{commentCount === 1 ? "" : "s"}
        </span>
      </div>
    </div>
  {/snippet}
</BentoCard>

<style>
  .review {
    display: grid;
    gap: 0.5rem;
    height: 100%;
  }
  .review-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .title,
  .title-button {
    font-weight: 600;
    color: var(--color-text);
    font-size: 0.95rem;
  }
  .title-button {
    background: none;
    border: none;
    padding: 0;
    text-align: left;
    cursor: pointer;
    font: inherit;
    text-decoration: underline;
  }
  .meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    flex-wrap: wrap;
    color: var(--color-text-muted);
    font-size: 0.8rem;
  }
  .comments {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
  }
</style>
