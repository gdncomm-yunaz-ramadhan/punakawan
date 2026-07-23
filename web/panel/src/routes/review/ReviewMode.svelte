<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import StickyActionBar from "../../lib/components/StickyActionBar.svelte";
  import BottomSheet from "../../lib/components/overlay/BottomSheet.svelte";
  import Dialog from "../../lib/components/overlay/Dialog.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import PlanDocument, { type SectionCommentRequest } from "../../lib/components/review/PlanDocument.svelte";
  import CommentRail from "../../lib/components/review/CommentRail.svelte";
  import AddCommentPopover from "../../lib/components/review/AddCommentPopover.svelte";
  import ReviewInstructionPanel from "../../lib/components/review/ReviewInstructionPanel.svelte";
  import ActiveRevisionSummary from "../../lib/components/review/ActiveRevisionSummary.svelte";
  import ProposalReview from "../../lib/components/review/ProposalReview.svelte";
  import { parseDocument, groupIntoSections, buildAnchor } from "../../lib/review/markdown";
  import {
    getArtifactCurrent,
    getReview,
    updateReview,
    listComments,
    createComment,
    updateComment,
    deleteComment,
    submitReview,
    cancelReview,
    getTimeline,
    type ArtifactContent,
    type ArtifactReview,
    type ArtifactComment,
    type ArtifactRevisionRequest,
    type RunReference,
  } from "../../lib/review/api";
  import { SessionExpiredError } from "../../lib/session";

  // Statuses nothing further will change - once observed, polling stops
  // (§4 "don't poll indefinitely ... since nothing further will change").
  const TERMINAL_STATUSES = new Set(["accepted", "rejected", "cancelled", "failed"]);
  const TIMELINE_POLL_MS = 8000;

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

  // Submit/cancel/timeline state (Phase 4: submit and retrigger).
  let submitting = $state(false);
  let submitError = $state<string | null>(null);
  let cancelling = $state(false);
  let cancelError = $state<string | null>(null);
  let cancelConfirmOpen = $state(false);
  let revisionRequest = $state<ArtifactRevisionRequest | null>(null);
  let run = $state<RunReference | null>(null);
  let pollTimer: ReturnType<typeof setInterval> | undefined;

  const sections = $derived(artifact ? groupIntoSections(parseDocument(artifact.content)) : []);
  const documentHeadingOrder = $derived(sections.map((s) => s.headingPath.join(" › ")));

  const isDraft = $derived(review?.metadata.status === "draft");
  // Once a proposal exists, ProposalReview takes over from
  // ActiveRevisionSummary - the remaining in-flight statuses
  // (submitted/queued/revising/awaiting_clarification, plus the terminal
  // cancelled/failed which never produce a proposal) have nothing to
  // review yet, so they keep the simpler status view.
  const PROPOSAL_STATUSES = new Set(["proposal_ready", "revision_requested", "accepted", "rejected", "conflicted"]);
  const hasProposal = $derived(review ? PROPOSAL_STATUSES.has(review.metadata.status) : false);
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
      if (r.metadata.status !== "draft") {
        startPollingIfNeeded();
      }
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  function stopPolling() {
    if (pollTimer !== undefined) {
      clearInterval(pollTimer);
      pollTimer = undefined;
    }
  }

  // Polls GET .../timeline on an interval so status changes made outside
  // this tab (or by a future agent) show up without a manual reload -
  // stops once a terminal status is observed, since nothing further will
  // change (§4). Restarting navigation to this route always re-fetches
  // from scratch (§5 "restart recovery"), so there is no separate
  // client-only state to reconnect to.
  async function pollTimeline() {
    try {
      const t = await getTimeline(reviewId);
      review = t.review;
      revisionRequest = t.revision_request ?? null;
      run = t.run ?? null;
      const commentList = await listComments(reviewId);
      comments = commentList.items;
      if (TERMINAL_STATUSES.has(t.review.metadata.status)) {
        stopPolling();
      }
    } catch {
      // A transient poll failure isn't worth surfacing as a page-level
      // error - the next tick (or a manual reload) tries again.
    }
  }

  function startPollingIfNeeded() {
    if (pollTimer !== undefined) return;
    if (!review || TERMINAL_STATUSES.has(review.metadata.status)) return;
    pollTimer = setInterval(pollTimeline, TIMELINE_POLL_MS);
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
    stopPolling();
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

  // Submits the review's current comment/instruction state for revision
  // (§8's Automatic Retrigger Flow). Guarded by `submitting` so a
  // double-click can't fire two requests - the backend is idempotent
  // either way, but this avoids redundant network chatter and a
  // confusing double-toast. Both 200 (idempotent replay) and 201 (fresh
  // submission) transition into the active-revision view identically.
  async function submitForRevision() {
    if (submitting || !review) return;
    submitting = true;
    submitError = null;
    try {
      const result = await submitReview(reviewId);
      revisionRequest = result.revision_request;
      run = result.run;
      // The backend flips status to "queued" on a fresh submit (and an
      // idempotent replay may have already advanced further) - refetch
      // via the timeline endpoint, the same real source of truth the
      // poll below uses, rather than assuming a status client-side.
      const t = await getTimeline(reviewId);
      review = t.review;
      revisionRequest = t.revision_request ?? revisionRequest;
      run = t.run ?? run;
      startPollingIfNeeded();
    } catch (e) {
      if (e instanceof SessionExpiredError) {
        sessionExpired = true;
      } else {
        submitError = e instanceof Error ? e.message : String(e);
      }
    } finally {
      submitting = false;
    }
  }

  function openCancelConfirm() {
    cancelError = null;
    cancelConfirmOpen = true;
  }

  function closeCancelConfirm() {
    cancelConfirmOpen = false;
  }

  async function confirmCancel() {
    if (cancelling) return;
    cancelling = true;
    cancelError = null;
    try {
      review = await cancelReview(reviewId);
      cancelConfirmOpen = false;
      stopPolling();
    } catch (e) {
      if (e instanceof SessionExpiredError) {
        sessionExpired = true;
        cancelConfirmOpen = false;
      } else {
        cancelError = e instanceof Error ? e.message : String(e);
      }
    } finally {
      cancelling = false;
    }
  }

  // ProposalReview's accept/reject/request-changes/rebase actions all
  // return a fresh ArtifactReview - refetch comments alongside it (a
  // request-changes' next attempt doesn't change comments, but rebase
  // returning to "draft" is exactly the restart-recovery path loadAll
  // already handles, so reusing the same refresh here keeps one code path
  // for "the review's server-side state moved, resync everything local").
  async function onProposalReviewChanged(updated: ArtifactReview) {
    review = updated;
    try {
      const commentList = await listComments(reviewId);
      comments = commentList.items;
    } catch {
      // Comments will resync on the next successful poll/reload - not
      // worth surfacing a page-level error over a post-action refresh.
    }
    if (TERMINAL_STATUSES.has(updated.metadata.status)) {
      stopPolling();
    } else {
      startPollingIfNeeded();
    }
  }

  // Answers a clarification question by reusing the existing comment
  // PATCH endpoint (no new comment-editing machinery) - "resolved_by_user"
  // is the status transition modeling "the user answered."
  async function answerClarification(commentId: string, body: string) {
    busyCommentId = commentId;
    try {
      const updated = await updateComment(reviewId, commentId, { body, status: "resolved_by_user" });
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

    {#if !isDraft && hasProposal}
      <!-- A proposal exists (or a further revision was requested on top
      of one) - hand off to the full review-of-the-proposal UI (diff,
      comment resolution, validation, lineage, accept/reject/request-
      changes/rebase) instead of the plainer in-flight status view. -->
      <ProposalReview
        {reviewId}
        {review}
        {comments}
        {isDesktop}
        onreviewChanged={onProposalReviewChanged}
      />
    {:else if !isDraft}
      <!-- Submitted (or later) - the document + comment-editing UI no
      longer applies (§8's freeze on submit), so this renders the
      active-revision status view instead. -->
      <ActiveRevisionSummary
        status={review.metadata.status}
        baseVersion={review.artifact.version}
        baseRevisionHash={review.artifact.revision_hash}
        {comments}
        {revisionRequest}
        {run}
        busy={cancelling}
        oncancel={openCancelConfirm}
        onanswerClarification={answerClarification}
      />
      {#if cancelError}
        <p class="error" role="alert">{cancelError}</p>
      {/if}
    {:else}
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

      {#if submitError}
        <p class="error" role="alert">{submitError}</p>
      {/if}

      <StickyActionBar>
        <button type="button" class="submit-review" disabled={submitting} onclick={submitForRevision}>
          {submitting ? "Submitting…" : "Submit Review"}
        </button>
      </StickyActionBar>
    {/if}
  {/if}
</div>

<Dialog open={cancelConfirmOpen} title="Cancel this review?" onclose={closeCancelConfirm}>
  <p>
    This will mark the review as cancelled. You can always start a new review for this artifact later, but this
    specific submission's tracked run will stop being followed.
  </p>
  {#if cancelError}
    <p class="error" role="alert">{cancelError}</p>
  {/if}
  <div class="dialog-actions">
    <button type="button" class="secondary-button" onclick={closeCancelConfirm} disabled={cancelling}>
      Keep Review
    </button>
    <button type="button" class="danger-button" onclick={confirmCancel} disabled={cancelling}>
      {cancelling ? "Cancelling…" : "Cancel Review"}
    </button>
  </div>
</Dialog>

<style>
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
    background: var(--color-accent);
    color: var(--color-accent-contrast);
    padding: 0.5rem 1rem;
    font-size: 0.9rem;
    cursor: pointer;
    min-height: 40px;
  }
  .submit-review:disabled {
    background: var(--color-border-strong);
    color: var(--color-surface);
    cursor: not-allowed;
  }
  .dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 1rem;
  }
  .secondary-button {
    border: 1px solid var(--color-border);
    border-radius: 6px;
    background: var(--color-surface);
    color: var(--color-text);
    padding: 0.5rem 1rem;
    font-size: 0.9rem;
    cursor: pointer;
    min-height: 40px;
  }
  .danger-button {
    border: none;
    border-radius: 6px;
    background: var(--color-danger);
    color: var(--color-accent-contrast);
    padding: 0.5rem 1rem;
    font-size: 0.9rem;
    cursor: pointer;
    min-height: 40px;
  }
  .secondary-button:disabled,
  .danger-button:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
</style>
