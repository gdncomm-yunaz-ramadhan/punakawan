<script lang="ts">
  // Artifact Review Phase 5 (frontend half, punokawan-apy.6): review-of-
  // the-proposal UI. Rendered by ReviewMode once a review has at least
  // one stored proposal (status proposal_ready/revision_requested/
  // accepted/rejected/conflicted) - see that component's own comment on
  // why this replaces ActiveRevisionSummary rather than living alongside
  // it once a proposal actually exists.
  import StickyActionBar from "../StickyActionBar.svelte";
  import Dialog from "../overlay/Dialog.svelte";
  import StatusBadge, { type BadgeVariant } from "../StatusBadge.svelte";
  import ErrorStateCard from "../cards/ErrorStateCard.svelte";
  import DiffViewer from "./DiffViewer.svelte";
  import VersionLineageGraphView from "../graphs/VersionLineageGraphView.svelte";
  import type { GraphNode, GraphEdge } from "../graphs/types";
  import {
    getProposalDiff,
    getProposalValidation,
    listProposals,
    acceptProposal,
    rejectProposal,
    requestChanges,
    rebaseReview,
    ApiError,
    type ArtifactComment,
    type ArtifactReview,
    type ArtifactRevisionProposal,
    type CommentResolution,
    type DiffLine,
    type DiffSummary,
    type StructuralReport,
    type ReviewComplianceReport,
  } from "../../review/api";
  import { SessionExpiredError } from "../../session";

  interface Props {
    reviewId: string;
    review: ArtifactReview;
    comments: ArtifactComment[];
    isDesktop: boolean;
    // Called after an action changes the review's own state (accept,
    // reject, request-changes dispatch, rebase) so the parent can refetch
    // review/timeline/comments from its single source of truth rather
    // than this component trying to keep its own copy in sync.
    onreviewChanged: (review: ArtifactReview) => void;
  }
  let { reviewId, review, comments, isDesktop, onreviewChanged }: Props = $props();

  let loading = $state(true);
  let loadError = $state<string | null>(null);

  let proposals = $state<ArtifactRevisionProposal[]>([]);
  let latestProposal = $state<ArtifactRevisionProposal | null>(null);
  let diffLines = $state<DiffLine[]>([]);
  let diffSummary = $state<DiffSummary | null>(null);
  let structural = $state<StructuralReport | null>(null);
  let compliance = $state<ReviewComplianceReport | null>(null);

  async function loadProposalData() {
    loading = true;
    loadError = null;
    try {
      const list = await listProposals(reviewId);
      proposals = list.items;
      const latest = list.items[list.items.length - 1] ?? null;
      latestProposal = latest;
      if (latest) {
        const attempt = String(latest.metadata.attempt);
        const [diff, validation] = await Promise.all([
          getProposalDiff(reviewId, attempt),
          getProposalValidation(reviewId, attempt),
        ]);
        diffLines = diff.lines;
        diffSummary = diff.summary;
        structural = validation.structural;
        compliance = validation.compliance;
      }
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    // Re-fetch whenever the parent hands us a proposal-bearing status -
    // a new attempt after "request changes" means a new latest proposal
    // exists to load, even though reviewId itself didn't change.
    void review.metadata.status;
    loadProposalData();
  });

  const resolutionByCommentId = $derived.by<Map<string, CommentResolution>>(() => {
    const map = new Map<string, CommentResolution>();
    for (const r of latestProposal?.results?.comment_resolutions ?? []) {
      map.set(r.comment_id, r);
    }
    return map;
  });

  const relevantComments = $derived(comments.filter((c) => c.status !== "obsolete"));

  const unresolvedCommentIds = $derived.by<Set<string>>(() => {
    const ids = new Set<string>(compliance?.unresolved_comment_ids ?? []);
    for (const c of relevantComments) {
      if (!resolutionByCommentId.has(c.id)) ids.add(c.id);
    }
    return ids;
  });

  const resolutionStatusLabel: Record<CommentResolution["status"], string> = {
    addressed: "Addressed",
    partially_addressed: "Partially addressed",
    rejected: "Rejected",
    not_applicable: "Not applicable",
  };
  const resolutionStatusVariant: Record<CommentResolution["status"], BadgeVariant> = {
    addressed: "success",
    partially_addressed: "warning",
    rejected: "danger",
    not_applicable: "neutral",
  };

  const lineageNodes = $derived.by<GraphNode[]>(() => {
    if (!latestProposal) return [];
    const nodes: GraphNode[] = [
      { id: "base", label: `v${latestProposal.base.version} (base)`, type: "version" },
    ];
    for (const p of proposals) {
      nodes.push({
        id: `attempt-${p.metadata.attempt}`,
        label: `Attempt ${p.metadata.attempt} · v${p.proposed.version}`,
        type: "proposal",
        data: { status: p.metadata.status },
      });
    }
    return nodes;
  });

  const lineageEdges = $derived.by<GraphEdge[]>(() => {
    if (proposals.length === 0) return [];
    const edges: GraphEdge[] = [
      { id: "edge-base-1", source: "base", target: "attempt-1", label: "proposed" },
    ];
    for (let i = 1; i < proposals.length; i++) {
      const from = proposals[i - 1].metadata.attempt;
      const to = proposals[i].metadata.attempt;
      edges.push({ id: `edge-${from}-${to}`, source: `attempt-${from}`, target: `attempt-${to}`, label: "revised" });
    }
    return edges;
  });

  const validationFailed = $derived(latestProposal?.results?.validation_status === "failed");
  const isConflicted = $derived(review.metadata.status === "conflicted");

  let actionError = $state<string | null>(null);
  let actionBusy = $state(false);
  let sessionExpired = $state(false);

  let acceptConfirmOpen = $state(false);
  let rejectConfirmOpen = $state(false);
  let requestChangesOpen = $state(false);
  let requestChangesInstruction = $state("");
  let showConflictFromAccept = $state(false);

  function handleActionError(e: unknown) {
    if (e instanceof SessionExpiredError) {
      sessionExpired = true;
      return;
    }
    actionError = e instanceof Error ? e.message : String(e);
  }

  async function confirmAccept() {
    if (!latestProposal || actionBusy) return;
    actionBusy = true;
    actionError = null;
    showConflictFromAccept = false;
    try {
      const result = await acceptProposal(reviewId, String(latestProposal.metadata.attempt));
      acceptConfirmOpen = false;
      onreviewChanged(result.review);
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        acceptConfirmOpen = false;
        showConflictFromAccept = true;
        // The backend already flipped the review to "conflicted" - refetch
        // isn't strictly required for this component's own state, but the
        // parent's review copy needs to catch up too so its status-gated
        // rendering (this component vs. the draft flow) matches reality.
        actionError = e.message;
      } else {
        handleActionError(e);
      }
    } finally {
      actionBusy = false;
    }
  }

  async function confirmReject() {
    if (!latestProposal || actionBusy) return;
    actionBusy = true;
    actionError = null;
    try {
      const updated = await rejectProposal(reviewId, String(latestProposal.metadata.attempt));
      rejectConfirmOpen = false;
      onreviewChanged(updated);
    } catch (e) {
      handleActionError(e);
    } finally {
      actionBusy = false;
    }
  }

  async function submitRequestChanges() {
    if (!latestProposal || actionBusy) return;
    actionBusy = true;
    actionError = null;
    try {
      const result = await requestChanges(
        reviewId,
        String(latestProposal.metadata.attempt),
        requestChangesInstruction.trim() || undefined,
      );
      requestChangesOpen = false;
      requestChangesInstruction = "";
      onreviewChanged(result.review);
    } catch (e) {
      handleActionError(e);
    } finally {
      actionBusy = false;
    }
  }

  async function doRebase() {
    if (actionBusy) return;
    actionBusy = true;
    actionError = null;
    try {
      const updated = await rebaseReview(reviewId);
      onreviewChanged(updated);
    } catch (e) {
      handleActionError(e);
    } finally {
      actionBusy = false;
    }
  }

  // Only "proposal_ready" has a decision actually pending on the shown
  // proposal - "revision_requested" means a newer attempt is already
  // dispatched (this view is showing the previous attempt read-only per
  // the phase brief), and conflicted/accepted/rejected are all terminal
  // or require rebase-first.
  const canAct = $derived(review.metadata.status === "proposal_ready");
</script>

<div data-testid="proposal-review">
  {#if sessionExpired}
    <ErrorStateCard
      title="Session expired"
      message="Your session has expired - reopen the panel from the terminal to continue."
    />
  {:else if loading}
    <p>Loading proposal…</p>
  {:else if loadError}
    <ErrorStateCard title="Failed to load proposal" message={loadError} />
  {:else if !latestProposal}
    <p>No proposal has been reported yet.</p>
  {:else}
    {#if isConflicted}
      <div class="conflict-banner" role="alert" data-testid="conflict-banner">
        <p>
          <strong>This proposal is out of date.</strong> The canonical artifact changed since this review's base version
          - accepting is blocked until you rebase onto the latest version. Rebasing returns this review to draft so you
          can review the refreshed document and resubmit.
        </p>
        <button type="button" class="primary-button" onclick={doRebase} disabled={actionBusy}>
          {actionBusy ? "Rebasing…" : "Rebase onto latest version"}
        </button>
      </div>
    {:else if showConflictFromAccept}
      <div class="conflict-banner" role="alert" data-testid="conflict-banner">
        <p>
          <strong>Accept failed: the canonical artifact changed underneath this proposal.</strong> Rebase to re-anchor
          this review at the current version, then review and resubmit.
        </p>
        <button type="button" class="primary-button" onclick={doRebase} disabled={actionBusy}>
          {actionBusy ? "Rebasing…" : "Rebase onto latest version"}
        </button>
      </div>
    {:else if review.metadata.status === "revision_requested"}
      <div class="inflight-banner" data-testid="revision-in-flight-banner">
        <p>
          Further changes were requested - a new revision attempt is in progress. The proposal below is the previous
          attempt, shown read-only until the next one is ready.
        </p>
      </div>
    {/if}

    <section class="summary-row">
      <div class="diff-badge" data-testid="diff-summary-badge">
        <span class="added">+{diffSummary?.added ?? 0}</span>
        <span class="removed">-{diffSummary?.removed ?? 0}</span>
      </div>
      <StatusBadge
        variant={review.metadata.status === "accepted" || review.metadata.status === "rejected" ? "neutral" : "info"}
        label={`Attempt ${latestProposal.metadata.attempt}`}
      />
      {#if latestProposal.proposed.change_summary}
        <p class="change-summary">{latestProposal.proposed.change_summary}</p>
      {/if}
    </section>

    <section class="panel-block">
      <h2>Diff</h2>
      <DiffViewer lines={diffLines} {isDesktop} />
    </section>

    <section class="panel-block">
      <h2>Comment resolution</h2>
      {#if relevantComments.length === 0}
        <p class="muted">No comments were on this review.</p>
      {:else}
        <ul class="resolution-list" data-testid="comment-resolution-list">
          {#each relevantComments as c (c.id)}
            {@const resolution = resolutionByCommentId.get(c.id)}
            {@const isUnresolved = unresolvedCommentIds.has(c.id)}
            <li class="resolution-item" class:unresolved={isUnresolved} data-testid="comment-resolution-item">
              <div class="resolution-head">
                <span class="comment-body">{c.body}</span>
                {#if resolution}
                  <StatusBadge
                    variant={resolutionStatusVariant[resolution.status]}
                    label={resolutionStatusLabel[resolution.status]}
                  />
                {:else if isUnresolved}
                  <StatusBadge variant="danger" label="Unresolved" />
                {/if}
              </div>
              {#if resolution?.explanation && (resolution.status === "rejected" || resolution.status === "partially_addressed")}
                <p class="explanation">{resolution.explanation}</p>
              {/if}
              {#if resolution?.status === "addressed" && resolution.changed_block_ids?.length}
                <p class="changed-blocks">
                  Changed blocks: {resolution.changed_block_ids.join(", ")}
                </p>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    <section class="panel-block">
      <h2>Validation</h2>
      <div class="validation-grid">
        <div class="validation-report">
          <StatusBadge
            variant={structural?.passed ? "success" : "danger"}
            label={structural?.passed ? "Structural: passed" : "Structural: failed"}
          />
          {#if structural?.issues.length}
            <ul class="issue-list">
              {#each structural.issues as issue}
                <li><strong>{issue.check}</strong>: {issue.message}</li>
              {/each}
            </ul>
          {/if}
        </div>
        <div class="validation-report">
          <StatusBadge
            variant={compliance?.passed ? "success" : "danger"}
            label={compliance?.passed ? "Review compliance: passed" : "Review compliance: failed"}
          />
          {#if compliance?.issues.length}
            <ul class="issue-list">
              {#each compliance.issues as issue}
                <li><strong>{issue.check}</strong>: {issue.message}</li>
              {/each}
            </ul>
          {/if}
        </div>
      </div>
      <StatusBadge
        variant={structural?.passed && compliance?.passed ? "success" : "danger"}
        label={structural?.passed && compliance?.passed ? "Overall: passed" : "Overall: failed"}
      />
    </section>

    <section class="panel-block">
      <h2>Version lineage</h2>
      <VersionLineageGraphView nodes={lineageNodes} edges={lineageEdges} title="Proposal lineage" />
    </section>

    {#if actionError && !showConflictFromAccept}
      <p class="error" role="alert">{actionError}</p>
    {/if}

    {#if canAct}
      <StickyActionBar>
        <button
          type="button"
          class="secondary-button"
          data-testid="request-changes-button"
          disabled={actionBusy}
          onclick={() => (requestChangesOpen = true)}
        >
          Request Changes
        </button>
        <button
          type="button"
          class="danger-button"
          data-testid="reject-button"
          disabled={actionBusy}
          onclick={() => (rejectConfirmOpen = true)}
        >
          Reject
        </button>
        <button
          type="button"
          class="primary-button"
          data-testid="accept-button"
          disabled={actionBusy || validationFailed}
          onclick={() => (acceptConfirmOpen = true)}
        >
          Accept
        </button>
      </StickyActionBar>
      {#if validationFailed}
        <p class="disabled-explanation" data-testid="accept-disabled-reason">
          Accept is disabled because this proposal failed validation - request changes to address the issues above
          instead.
        </p>
      {/if}
    {/if}
  {/if}
</div>

<Dialog open={acceptConfirmOpen} title="Accept this proposal?" onclose={() => (acceptConfirmOpen = false)}>
  <p>
    Accepting creates a new canonical version of the artifact from this proposal's content. This cannot be undone
    from here - you would need to start a new review to revert further changes.
  </p>
  {#if actionError}
    <p class="error" role="alert">{actionError}</p>
  {/if}
  <div class="dialog-actions">
    <button type="button" class="secondary-button" onclick={() => (acceptConfirmOpen = false)} disabled={actionBusy}>
      Cancel
    </button>
    <button type="button" class="primary-button" onclick={confirmAccept} disabled={actionBusy}>
      {actionBusy ? "Accepting…" : "Accept Proposal"}
    </button>
  </div>
</Dialog>

<Dialog open={rejectConfirmOpen} title="Reject this proposal?" onclose={() => (rejectConfirmOpen = false)}>
  <p>
    This marks the review as rejected. The canonical artifact is never touched by a rejection - only this review's
    own status changes.
  </p>
  {#if actionError}
    <p class="error" role="alert">{actionError}</p>
  {/if}
  <div class="dialog-actions">
    <button type="button" class="secondary-button" onclick={() => (rejectConfirmOpen = false)} disabled={actionBusy}>
      Keep Reviewing
    </button>
    <button type="button" class="danger-button" onclick={confirmReject} disabled={actionBusy}>
      {actionBusy ? "Rejecting…" : "Reject Proposal"}
    </button>
  </div>
</Dialog>

<Dialog open={requestChangesOpen} title="Request further changes" onclose={() => (requestChangesOpen = false)}>
  <p>Optionally describe what should change. This dispatches another revision attempt under this same review.</p>
  <textarea
    class="instruction-input"
    placeholder="Additional guidance for the next attempt (optional)…"
    aria-label="Additional guidance for the next attempt"
    bind:value={requestChangesInstruction}
    data-testid="request-changes-input"
  ></textarea>
  {#if actionError}
    <p class="error" role="alert">{actionError}</p>
  {/if}
  <div class="dialog-actions">
    <button type="button" class="secondary-button" onclick={() => (requestChangesOpen = false)} disabled={actionBusy}>
      Cancel
    </button>
    <button type="button" class="primary-button" onclick={submitRequestChanges} disabled={actionBusy}>
      {actionBusy ? "Submitting…" : "Request Changes"}
    </button>
  </div>
</Dialog>

<style>
  .conflict-banner {
    background: var(--color-accent-soft);
    border: 1px solid var(--color-warning);
    border-radius: var(--radius-card);
    padding: 1rem 1.25rem;
    margin-bottom: 1rem;
    display: grid;
    gap: 0.6rem;
  }
  .conflict-banner p {
    margin: 0;
    color: var(--color-text);
    font-size: 0.88rem;
  }
  .inflight-banner {
    background: var(--color-surface-subtle);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    padding: 0.85rem 1.1rem;
    margin-bottom: 1rem;
  }
  .inflight-banner p {
    margin: 0;
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
  .summary-row {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    flex-wrap: wrap;
    margin-bottom: 1rem;
  }
  .diff-badge {
    display: inline-flex;
    gap: 0.5rem;
    font-weight: 700;
    font-family: monospace;
    font-size: 0.9rem;
  }
  .diff-badge .added {
    color: var(--color-success);
  }
  .diff-badge .removed {
    color: var(--color-danger);
  }
  .change-summary {
    margin: 0;
    color: var(--color-text-muted);
    font-size: 0.85rem;
    flex-basis: 100%;
  }
  .panel-block {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 1rem 1.25rem;
    margin-bottom: 1rem;
  }
  .panel-block h2 {
    font-size: 0.95rem;
    margin: 0 0 0.6rem;
    color: var(--color-text);
  }
  .muted {
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
  .resolution-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 0.6rem;
  }
  .resolution-item {
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: 0.5rem 0.75rem;
  }
  .resolution-item.unresolved {
    border-color: var(--color-danger);
  }
  .resolution-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 0.5rem;
  }
  .comment-body {
    font-size: 0.85rem;
    color: var(--color-text);
  }
  .explanation,
  .changed-blocks {
    margin: 0.35rem 0 0;
    font-size: 0.8rem;
    color: var(--color-text-muted);
  }
  .validation-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1rem;
    margin-bottom: 0.75rem;
  }
  .validation-report {
    display: grid;
    gap: 0.4rem;
    align-content: start;
  }
  .issue-list {
    margin: 0;
    padding-left: 1.1rem;
    font-size: 0.8rem;
    color: var(--color-text);
  }
  .error {
    color: var(--color-danger);
    font-size: 0.85rem;
  }
  .disabled-explanation {
    color: var(--color-text-muted);
    font-size: 0.8rem;
    margin: 0.4rem 0 0;
  }
  .primary-button {
    border: none;
    border-radius: 6px;
    background: var(--color-accent);
    color: var(--color-accent-contrast);
    padding: 0.5rem 1rem;
    font-size: 0.9rem;
    cursor: pointer;
    min-height: 44px;
  }
  .secondary-button {
    border: 1px solid var(--color-border);
    border-radius: 6px;
    background: var(--color-surface);
    color: var(--color-text);
    padding: 0.5rem 1rem;
    font-size: 0.9rem;
    cursor: pointer;
    min-height: 44px;
  }
  .danger-button {
    border: none;
    border-radius: 6px;
    background: var(--color-danger);
    color: var(--color-accent-contrast);
    padding: 0.5rem 1rem;
    font-size: 0.9rem;
    cursor: pointer;
    min-height: 44px;
  }
  .primary-button:disabled,
  .secondary-button:disabled,
  .danger-button:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
  .dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 1rem;
  }
  .instruction-input {
    width: 100%;
    box-sizing: border-box;
    min-height: 4.5rem;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: 0.5rem;
    font-family: inherit;
    font-size: 0.85rem;
    color: var(--color-text);
    background: var(--color-surface);
    resize: vertical;
    margin: 0.75rem 0;
  }
</style>
