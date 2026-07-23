<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import StickyActionBar from "../../lib/components/StickyActionBar.svelte";
  import BottomSheet from "../../lib/components/overlay/BottomSheet.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import PlanDocument, { type SectionCommentRequest } from "../../lib/components/review/PlanDocument.svelte";
  import CommentRail from "../../lib/components/review/CommentRail.svelte";
  import AddCommentPopover from "../../lib/components/review/AddCommentPopover.svelte";
  import ReviewInstructionPanel from "../../lib/components/review/ReviewInstructionPanel.svelte";
  import { parseDocument, groupIntoSections, buildAnchor } from "../../lib/review/markdown";
  import {
    getArtifactCurrent,
    getReview,
    updateReview,
    listComments,
    createComment,
    updateComment,
    deleteComment,
    type ArtifactContent,
    type ArtifactReview,
    type ArtifactComment,
  } from "../../lib/review/api";
  import { SessionExpiredError } from "../../lib/session";

  interface Props {
    reviewId: string;
    // Test-only seam mirroring DataTable's forceWidth pattern: jsdom has
    // no layout engine, so tests force the desktop/mobile branch
    // deterministically instead of faking a real viewport resize.
    forceWidth?: number;
  }
  let { reviewId, forceWidth }: Props = $props();

  const DESKTOP_BREAKPOINT = 1024;

  let observedWidth = $state<number | null>(null);
  let isDesktop = $derived((forceWidth ?? observedWidth ?? DESKTOP_BREAKPOINT) >= DESKTOP_BREAKPOINT);

  function updateObservedWidth() {
    if (typeof window !== "undefined") observedWidth = window.innerWidth;
  }

  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let sessionExpired = $state(false);

  let review = $state<ArtifactReview | null>(null);
  let artifact = $state<ArtifactContent | null>(null);
  let comments = $state<ArtifactComment[]>([]);

  let pendingAnchor = $state<SectionCommentRequest | null>(null);
  let commentDraftDirty = $state(false);
  let submittingComment = $state(false);
  let commentError = $state<string | null>(null);
  let busyCommentId = $state<string | null>(null);

  let mobileSheetOpen = $state(false);

  let instructionPanel: ReviewInstructionPanel | undefined = $state();

  const sections = $derived(artifact ? groupIntoSections(parseDocument(artifact.content)) : []);
  const documentHeadingOrder = $derived(sections.map((s) => s.headingPath.join(" › ")));

  const isDraft = $derived(review?.metadata.status === "draft");
  const shortHash = $derived(
    artifact?.reference.revision_hash ? artifact.reference.revision_hash.replace(/^sha256:/, "").slice(0, 10) : "",
  );

  const openCommentCount = $derived(comments.filter((c) => c.status !== "obsolete").length);

  function hasUnsavedChanges(): boolean {
    return commentDraftDirty || (instructionPanel?.hasUnsavedChanges() ?? false);
  }

  async function loadAll() {
    loading = true;
    loadError = null;
    try {
      const r = await getReview(reviewId);
      review = r;
      const [content, commentList] = await Promise.all([
        getArtifactCurrent(r.artifact.type, r.artifact.id),
        listComments(reviewId),
      ]);
      artifact = content;
      comments = commentList.items;
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  function beforeUnloadHandler(e: BeforeUnloadEvent) {
    if (hasUnsavedChanges()) {
      e.preventDefault();
      e.returnValue = "";
    }
  }

  onMount(() => {
    updateObservedWidth();
    window.addEventListener("resize", updateObservedWidth);
    window.addEventListener("beforeunload", beforeUnloadHandler);
    // Always refetch on route entry (§13.10 "clear exit and resume
    // behavior") - nothing about this component caches across mounts.
    loadAll();
  });

  onDestroy(() => {
    if (typeof window !== "undefined") {
      window.removeEventListener("resize", updateObservedWidth);
      window.removeEventListener("beforeunload", beforeUnloadHandler);
    }
  });

  async function saveInstruction(instruction: string) {
    if (!review) return;
    const updated = await updateReview(reviewId, { instruction });
    review = updated;
  }

  function openSectionComment(req: SectionCommentRequest) {
    pendingAnchor = req;
    commentDraftDirty = true;
    commentError = null;
    mobileSheetOpen = true;
  }

  function cancelComment() {
    pendingAnchor = null;
    commentDraftDirty = false;
    commentError = null;
  }

  async function submitComment(body: string) {
    if (!pendingAnchor || !artifact || !review) return;
    submittingComment = true;
    commentError = null;
    const clientId = crypto.randomUUID();
    const anchor = buildAnchor({
      baseRevisionHash: artifact.reference.revision_hash,
      headingPath: pendingAnchor.headingPath,
      quotedText: pendingAnchor.quotedText,
    });
    try {
      const comment = await createComment(reviewId, { id: clientId, anchor, body });
      comments = [...comments, comment];
      pendingAnchor = null;
      commentDraftDirty = false;
    } catch (e) {
      if (e instanceof SessionExpiredError) {
        sessionExpired = true;
      } else {
        commentError = e instanceof Error ? e.message : String(e);
      }
    } finally {
      submittingComment = false;
    }
  }

  async function editComment(commentId: string, body: string) {
    busyCommentId = commentId;
    try {
      const updated = await updateComment(reviewId, commentId, { body });
      comments = comments.map((c) => (c.id === commentId ? updated : c));
    } catch (e) {
      if (e instanceof SessionExpiredError) {
        sessionExpired = true;
      } else {
        commentError = e instanceof Error ? e.message : String(e);
      }
    } finally {
      busyCommentId = null;
    }
  }

  async function removeComment(commentId: string) {
    busyCommentId = commentId;
    try {
      await deleteComment(reviewId, commentId);
      comments = comments.map((c) => (c.id === commentId ? { ...c, status: "obsolete" as const } : c));
    } catch (e) {
      if (e instanceof SessionExpiredError) {
        sessionExpired = true;
      } else {
        commentError = e instanceof Error ? e.message : String(e);
      }
    } finally {
      busyCommentId = null;
    }
  }
</script>

<div data-testid="review-mode" data-layout={isDesktop ? "desktop" : "mobile"}>
  {#if sessionExpired}
    <ErrorStateCard
      title="Session expired"
      message="Your session has expired - reopen the panel from the terminal to continue."
    />
  {:else if loading}
    <p>Loading review…</p>
  {:else if loadError}
    <ErrorStateCard title="Failed to load review" message={loadError} />
  {:else if review && artifact}
    <PageHeader
      title={review.review.title}
      description={`Reviewing ${review.artifact.type} ${review.artifact.id} · version ${review.artifact.version} · ${shortHash}`}
    />

    {#if !isDraft}
      <p class="readonly-note" role="status">
        This review is {review.metadata.status.replace(/_/g, " ")} and is read-only in this phase's UI.
      </p>
    {/if}

    {#if commentDraftDirty && pendingAnchor}
      <AddCommentPopover
        headingPath={pendingAnchor.headingPath}
        quotedText={pendingAnchor.quotedText}
        submitting={submittingComment}
        onsubmit={submitComment}
        oncancel={cancelComment}
      />
    {/if}
    {#if commentError}
      <p class="error" role="alert">{commentError}</p>
    {/if}

    <ReviewInstructionPanel
      bind:this={instructionPanel}
      instruction={review.review.instruction ?? ""}
      onsave={saveInstruction}
    />

    {#if isDesktop}
      <div class="two-pane">
        <div class="document-pane">
          <PlanDocument
            content={artifact.content}
            onCommentSection={openSectionComment}
            onCommentSelection={openSectionComment}
          />
        </div>
        <div class="comment-pane">
          <h2 class="rail-heading">Comments ({openCommentCount})</h2>
          <CommentRail
            {comments}
            {documentHeadingOrder}
            editable={isDraft}
            {busyCommentId}
            onEditComment={editComment}
            onDeleteComment={removeComment}
          />
        </div>
      </div>
    {:else}
      <div class="mobile-document">
        <PlanDocument
          content={artifact.content}
          onCommentSection={openSectionComment}
          onCommentSelection={openSectionComment}
        />
      </div>
      <button
        type="button"
        class="fab"
        data-testid="view-comments-toggle"
        onclick={() => (mobileSheetOpen = true)}
      >
        View Comments ({openCommentCount})
      </button>
      <BottomSheet open={mobileSheetOpen} title="Comments" onclose={() => (mobileSheetOpen = false)}>
        <CommentRail
          {comments}
          {documentHeadingOrder}
          editable={isDraft}
          {busyCommentId}
          onEditComment={editComment}
          onDeleteComment={removeComment}
        />
      </BottomSheet>
    {/if}

    <StickyActionBar>
      <button type="button" class="submit-review" disabled title="Submitting is implemented in the next phase">
        Submit Review
      </button>
    </StickyActionBar>
  {/if}
</div>

<style>
  .readonly-note {
    background: var(--color-accent-soft);
    color: var(--color-text);
    border-radius: 6px;
    padding: 0.5rem 0.75rem;
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  .error {
    color: var(--color-danger);
    font-size: 0.85rem;
  }
  .two-pane {
    display: grid;
    grid-template-columns: 1fr 380px;
    gap: 1.25rem;
    align-items: start;
    margin: 1rem 0;
  }
  .document-pane {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 1.25rem 1.5rem;
    max-height: 75vh;
    overflow-y: auto;
  }
  .comment-pane {
    position: sticky;
    top: 1rem;
    max-height: 75vh;
    overflow-y: auto;
  }
  .rail-heading {
    font-size: 0.9rem;
    margin: 0 0 0.6rem;
    color: var(--color-text);
  }
  .mobile-document {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    padding: 1rem;
    margin: 1rem 0 4.5rem;
  }
  .fab {
    position: fixed;
    right: 1rem;
    bottom: 5rem;
    z-index: 16;
    border: none;
    border-radius: 999px;
    background: var(--color-accent);
    color: var(--color-accent-contrast);
    padding: 0.75rem 1.1rem;
    font-size: 0.85rem;
    font-weight: 600;
    box-shadow: var(--shadow-card);
    cursor: pointer;
    min-height: 44px;
  }
  .submit-review {
    border: none;
    border-radius: 6px;
    background: var(--color-border-strong);
    color: var(--color-surface);
    padding: 0.5rem 1rem;
    font-size: 0.9rem;
    cursor: not-allowed;
    min-height: 40px;
  }
</style>
