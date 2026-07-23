<script lang="ts">
  // Active-revision status view (Artifact Review Phase 4 - submit and
  // retrigger, frontend half): shown once a review is no longer "draft".
  // Renders the review's real, current submission-lifecycle state from
  // GET /api/v1/reviews/{reviewId}/timeline - there is no separate
  // client-side state machine here, this is a read-only reflection of
  // whatever the backend last reported.
  import BentoGrid from "../cards/BentoGrid.svelte";
  import BentoCard from "../cards/BentoCard.svelte";
  import StatusCard, { type StatusVariant } from "../cards/StatusCard.svelte";
  import MetricCard from "../cards/MetricCard.svelte";
  import EmptyStateCard from "../cards/EmptyStateCard.svelte";
  import StatusBadge from "../StatusBadge.svelte";
  import CommentResolutionChart, { type CommentResolutionSnapshot } from "../charts/CommentResolutionChart.svelte";
  import type { ArtifactComment, ArtifactRevisionRequest, ReviewStatus, RunReference } from "../../review/api";

  interface Props {
    status: ReviewStatus;
    baseVersion: number;
    baseRevisionHash: string;
    comments: ArtifactComment[];
    revisionRequest?: ArtifactRevisionRequest | null;
    run?: RunReference | null;
    // Whether a cancel/answer-clarification action is currently in
    // flight, so the parent can disable buttons and avoid double-submits.
    busy?: boolean;
    oncancel?: () => void;
    onanswerClarification?: (commentId: string, body: string) => void;
  }
  let {
    status,
    baseVersion,
    baseRevisionHash,
    comments,
    revisionRequest = null,
    run = null,
    busy = false,
    oncancel,
    onanswerClarification,
  }: Props = $props();

  // Per-status semantic variant + short human-readable explanation. Every
  // status in ReviewStatus (minus "draft", which never reaches this view)
  // must render sensibly - including ones that, right now, are never
  // actually reached in practice (proposal_ready, revision_requested,
  // accepted, rejected) because the agent side of this loop is an
  // intentionally out-of-scope follow-up. This is not fake progress
  // simulation - it's just a complete, honest label table.
  const STATUS_INFO: Record<Exclude<ReviewStatus, "draft">, { variant: StatusVariant; description: string }> = {
    submitted: { variant: "info", description: "Submission received, about to be queued." },
    queued: { variant: "info", description: "Waiting for Punakawan to start revising." },
    revising: { variant: "info", description: "Punakawan is revising the artifact." },
    awaiting_clarification: {
      variant: "warning",
      description: "Punakawan needs clarification before continuing.",
    },
    proposal_ready: { variant: "success", description: "A proposed revision is ready to review." },
    revision_requested: { variant: "info", description: "Further changes were requested on the proposal." },
    accepted: { variant: "success", description: "The revision was accepted." },
    rejected: { variant: "danger", description: "The revision was rejected." },
    cancelled: { variant: "warning", description: "This review was cancelled." },
    failed: { variant: "danger", description: "The revision run failed." },
    conflicted: {
      variant: "warning",
      description: "The artifact changed since this review was based - needs a rebase.",
    },
  };

  const TERMINAL_STATUSES: ReadonlySet<ReviewStatus> = new Set(["accepted", "rejected", "cancelled", "failed"]);

  // draft can't actually appear here (ReviewMode only mounts this once
  // status !== "draft"), but the type is total so we need a fallback.
  const info = $derived(STATUS_INFO[status as Exclude<ReviewStatus, "draft">] ?? STATUS_INFO.queued);
  const statusLabel = $derived(status.replace(/_/g, " "));

  const isTerminal = $derived(TERMINAL_STATUSES.has(status));
  const canCancel = $derived(!isTerminal && status !== "cancelled");

  function truncateHash(hash: string): string {
    const bare = hash.replace(/^sha256:/, "");
    return bare.length > 12 ? `${bare.slice(0, 12)}…` : bare;
  }

  // Group the review's real comments by status into the fixed bucket
  // shape CommentResolutionChart already supports (open/addressed/
  // resolved/won't-fix, one "period" data point) - the chart component
  // itself is a reused, untouched dependency from the charts phase, so
  // the 7-way CommentStatus enum folds into its 4 buckets rather than
  // the chart being extended.
  const commentSnapshot = $derived.by<CommentResolutionSnapshot>(() => {
    let open = 0;
    let addressed = 0;
    let resolved = 0;
    let wontfix = 0;
    for (const c of comments) {
      switch (c.status) {
        case "open":
        case "needs_clarification":
          open += 1;
          break;
        case "addressed":
        case "partially_addressed":
          addressed += 1;
          break;
        case "resolved_by_user":
          resolved += 1;
          break;
        case "rejected_by_agent":
        case "obsolete":
          wontfix += 1;
          break;
      }
    }
    return { period: "Current", open, addressed, resolved, wontfix };
  });

  const clarificationComments = $derived(comments.filter((c) => c.status === "needs_clarification"));

  let clarificationDrafts = $state<Record<string, string>>({});

  function draftFor(comment: ArtifactComment): string {
    return clarificationDrafts[comment.id] ?? comment.body;
  }

  function updateDraft(commentId: string, value: string) {
    clarificationDrafts = { ...clarificationDrafts, [commentId]: value };
  }

  function submitClarification(comment: ArtifactComment) {
    onanswerClarification?.(comment.id, draftFor(comment));
  }
</script>

<div data-testid="active-revision-summary">
  <div class="toolbar">
    <StatusBadge variant={info.variant} label={statusLabel} />
    {#if canCancel}
      <button type="button" class="cancel-button" data-testid="cancel-review-button" disabled={busy} onclick={oncancel}>
        Cancel Review
      </button>
    {/if}
  </div>

  {#if clarificationComments.length > 0}
    <section class="clarifications" data-testid="clarification-section" aria-label="Needs your input">
      <h2>Needs your input</h2>
      {#each clarificationComments as comment (comment.id)}
        <div class="clarification-item" data-testid="clarification-item">
          <p class="clarification-body">{comment.body}</p>
          <textarea
            class="clarification-input"
            data-testid={`clarification-input-${comment.id}`}
            value={draftFor(comment)}
            oninput={(e) => updateDraft(comment.id, (e.target as HTMLTextAreaElement).value)}
          ></textarea>
          <button
            type="button"
            data-testid={`clarification-save-${comment.id}`}
            disabled={busy}
            onclick={() => submitClarification(comment)}
          >
            Save Answer
          </button>
        </div>
      {/each}
    </section>
  {/if}

  <BentoGrid>
    <StatusCard variant={info.variant} label={statusLabel} description={info.description} size="medium" />

    <MetricCard label="Base version" value={`v${baseVersion} · ${truncateHash(baseRevisionHash)}`} size="medium" />

    <MetricCard label="Comments" value={comments.length} size="small" />

    {#if revisionRequest}
      <BentoCard size="wide">
        {#snippet children()}
          <div class="revision-request">
            <h3>Revision request</h3>
            <dl>
              <dt>Submitted at</dt>
              <dd>{new Date(revisionRequest.metadata.submitted_at).toLocaleString()}</dd>
              <dt>Submitted by</dt>
              <dd>{revisionRequest.metadata.submitted_by}</dd>
              {#if run}
                <dt>Tracked as</dt>
                <dd><code>{run.run_id}</code></dd>
              {/if}
            </dl>
          </div>
        {/snippet}
      </BentoCard>
    {:else}
      <EmptyStateCard
        title="No revision request yet"
        message="This review hasn't been submitted for revision yet."
      />
    {/if}

    <BentoCard size="wide">
      {#snippet children()}
        <CommentResolutionChart snapshots={[commentSnapshot]} title="Comment status breakdown" />
      {/snippet}
    </BentoCard>

    <EmptyStateCard
      title="No execution evidence yet"
      message="This run hasn't been picked up by an agent."
    />
  </BentoGrid>
</div>

<style>
  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    margin-bottom: 0.75rem;
  }
  .cancel-button {
    border: 1px solid var(--color-danger);
    border-radius: 6px;
    background: var(--color-surface);
    color: var(--color-danger);
    padding: 0.5rem 1rem;
    font-size: 0.9rem;
    cursor: pointer;
    min-height: 40px;
  }
  .cancel-button:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
  .clarifications {
    background: var(--color-accent-soft);
    border-radius: var(--radius-card);
    padding: 1rem 1.25rem;
    margin-bottom: 1rem;
  }
  .clarifications h2 {
    margin: 0 0 0.6rem;
    font-size: 0.95rem;
    color: var(--color-text);
  }
  .clarification-item {
    display: grid;
    gap: 0.4rem;
    margin-bottom: 0.75rem;
  }
  .clarification-body {
    margin: 0;
    color: var(--color-text);
    font-size: 0.85rem;
  }
  .clarification-input {
    width: 100%;
    box-sizing: border-box;
    min-height: 3.5rem;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: 0.5rem;
    font-family: inherit;
    font-size: 0.85rem;
    color: var(--color-text);
    background: var(--color-surface);
    resize: vertical;
  }
  .revision-request h3 {
    margin: 0 0 0.5rem;
    font-size: 0.9rem;
    color: var(--color-text);
  }
  .revision-request dl {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0.25rem 0.75rem;
    margin: 0;
    font-size: 0.85rem;
  }
  .revision-request dt {
    color: var(--color-text-muted);
  }
  .revision-request dd {
    margin: 0;
    color: var(--color-text);
    word-break: break-all;
  }
</style>
