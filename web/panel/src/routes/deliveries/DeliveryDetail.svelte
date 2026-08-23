<script lang="ts">
  import { onMount } from "svelte";
  import {
    getDeliveryView,
    cancelDelivery,
    deliveryEvidenceUrl,
    type DeliveryView,
    type DeliveryLaneStatus,
    type DeliveryLaneSummary,
  } from "../../lib/api/client";
  import { onPanelEvent, parsePanelEvent } from "../../lib/events/sse.svelte";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import Card from "../../lib/components/cards/Card.svelte";
  import BentoGrid from "../../lib/components/cards/BentoGrid.svelte";
  import MetricCard from "../../lib/components/cards/MetricCard.svelte";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import Button from "../../lib/components/Button.svelte";
  import DeliveryQuestionForm from "./DeliveryQuestionForm.svelte";
  import DeliveryApprovalCard from "./DeliveryApprovalCard.svelte";
  import { deliveryLabel } from "./deliveryList";
  import DeliveryCancelDialog from "./DeliveryCancelDialog.svelte";

  interface Props {
    orchestrationId: string;
  }
  let { orchestrationId }: Props = $props();

  let view: DeliveryView | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(true);
  let lastSeq = 0;
  let newlyRunnable: Set<string> = $state(new Set());
  let approvedBy = $state("");
  let cancelling = $state(false);
  let cancelError: string | null = $state(null);

  async function load(id: string) {
    loading = true;
    error = null;
    newlyRunnable = new Set();
    try {
      const v = await getDeliveryView(id);
      view = v;
      lastSeq = v.latest_seq;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  // Only a delivery.updated SSE event for this orchestration triggers a
  // since_seq refetch - a plain unconditional reload (like most other
  // routes' onPanelEvent handlers) would blow away newly_runnable_lane_ids
  // every time any unrelated panel event fires, since a from-scratch
  // GetDeliveryView call always leaves that diff empty.
  async function refresh(id: string) {
    try {
      const v = await getDeliveryView(id, lastSeq);
      view = v;
      lastSeq = v.latest_seq;
      newlyRunnable = new Set(v.newly_runnable_lane_ids);
      setTimeout(() => {
        newlyRunnable = new Set();
      }, 8000);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  onMount(() => {
    return onPanelEvent((evt) => {
      if (evt.type !== "delivery.updated") return;
      const parsed = parsePanelEvent(evt);
      if (parsed?.entity_id !== orchestrationId) return;
      refresh(orchestrationId);
    });
  });

  // Single trigger for the first load and for a later orchestrationId change -
  // effects run after the first render too, so loading from onMount as well
  // would fire the same request twice per open.
  $effect(() => {
    load(orchestrationId);
  });

  function applyUpdate(v: DeliveryView) {
    view = v;
    lastSeq = v.latest_seq;
  }

  // Cancelling is irreversible, so it goes through the same confirmation the
  // deliveries list uses rather than firing on a single click.
  let confirmingCancel = $state(false);

  function startCancel() {
    cancelError = null;
    confirmingCancel = true;
  }

  function closeCancel() {
    if (cancelling) return;
    confirmingCancel = false;
    cancelError = null;
  }

  async function cancel() {
    if (!view) return;
    cancelling = true;
    cancelError = null;
    try {
      applyUpdate(await cancelDelivery(orchestrationId, { expected_revision: view.orchestration.revision }));
      confirmingCancel = false;
    } catch (e) {
      cancelError = e instanceof Error ? e.message : String(e);
    } finally {
      cancelling = false;
    }
  }

  function lanesFor(projectId: string): DeliveryLaneSummary[] {
    return view?.lanes.filter((l) => l.project_id === projectId) ?? [];
  }

  // stageLabel names how far a lane's current attempt has progressed
  // through the fixed Semar -> Gareng -> Petruk -> Bagong pipeline, e.g.
  // "Semar → Gareng" once Gareng's review is recorded but Petruk's plan
  // isn't yet - null before any stage has been recorded at all.
  function stageLabel(lane: DeliveryLaneSummary): string | null {
    const stages: [string, string | undefined][] = [
      ["Semar", lane.semar_record_id],
      ["Gareng", lane.gareng_record_id],
      ["Petruk", lane.petruk_record_id],
      ["Bagong", lane.bagong_record_id],
    ];
    const reached = stages.filter(([, id]) => id).map(([name]) => name);
    return reached.length ? reached.join(" → ") : null;
  }

  function shortSha(sha: string): string {
    return sha.slice(0, 8);
  }

  const orchestrationStatusVariants: Record<string, BadgeVariant> = {
    pending: "neutral",
    active: "info",
    completed: "success",
    cancelled: "danger",
  };

  const laneStatusVariants: Record<DeliveryLaneStatus, BadgeVariant> = {
    waiting: "neutral",
    blocked: "warning",
    runnable: "info",
    leased: "info",
    running: "info",
    review: "warning",
    accepted: "success",
    failed: "danger",
  };
</script>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <ErrorStateCard title="Failed to load delivery" message={error} />
{:else if view}
  {@const v = view}
  <PageHeader title={deliveryLabel(v.orchestration, v)} description={v.next_action}>
    {#snippet actions()}
      {#if v.orchestration.status === "pending" || v.orchestration.status === "active"}
        <Button variant="danger" size="sm" disabled={cancelling} onclick={startCancel}>Cancel delivery</Button>
      {/if}
    {/snippet}
  </PageHeader>

  <DeliveryCancelDialog
    open={confirmingCancel}
    label={deliveryLabel(v.orchestration, v)}
    orchestrationId={orchestrationId}
    busy={cancelling}
    error={cancelError}
    onclose={closeCancel}
    onconfirm={cancel}
  />

  <!-- A cancel failure while the dialog is closed (it closes on success) still
       needs somewhere to show. -->
  {#if cancelError && !confirmingCancel}
    <p role="alert" class="error">{cancelError}</p>
  {/if}

  <div class="status-row">
    <StatusBadge
      variant={orchestrationStatusVariants[v.orchestration.status] ?? "neutral"}
      label={v.orchestration.status}
    />
    <!-- The id is no longer the page title, so it is surfaced here (in a
         monospace run that selects cleanly) to stay copyable for CLI use. -->
    <span class="meta"><code class="id">{orchestrationId}</code></span>
    <span class="meta">revision {v.orchestration.revision} · latest seq {v.latest_seq}</span>
  </div>

  <!-- Prose is never derived, so a delivery nobody wrote a description for has
       nothing to show here and the block is left out entirely rather than
       rendering an empty paragraph. -->
  {#if v.description}
    <p class="description" data-testid="delivery-description">{v.description}</p>
  {/if}

  <!-- The session and the plan record are questions a reader comes here to
       answer either way, so both rows always render and an unrecorded one says
       so in muted text - unlike the description, which simply disappears. -->
  <dl class="references" data-testid="delivery-references">
    <dt>Session</dt>
    <dd>
      {#if v.session_id}
        <code class="id">{v.session_id}</code>
      {:else}
        <span class="unset">Not recorded</span>
      {/if}
    </dd>
    <dt>Plan record</dt>
    <dd>
      {#if v.plan_record_id}
        <code class="id">{v.plan_record_id}</code>
      {:else}
        <span class="unset">Not recorded</span>
      {/if}
    </dd>
  </dl>

  <BentoGrid>
    <MetricCard size="small" columns={3} label="Projects" value={v.projects.length} />
    <MetricCard size="small" columns={3} label="Lanes" value={v.lanes.length} />
    <MetricCard
      size="small"
      columns={3}
      label="Blocked lanes"
      value={v.blockers.length}
      accent={v.blockers.length > 0 ? "danger" : "none"}
    />
    <MetricCard
      size="small"
      columns={3}
      label="Pending approvals"
      value={v.pending_approvals.length}
      accent={v.pending_approvals.length > 0 ? "gold" : "none"}
    />
  </BentoGrid>

  {#if v.pending_questions.length > 0}
    <section aria-labelledby="questions-heading">
      <h2 id="questions-heading">Pending questions</h2>
      <ul class="questions">
        {#each v.pending_questions as question (question)}
          <li>
            <Card>
              {#snippet children()}
                <DeliveryQuestionForm
                  {orchestrationId}
                  {question}
                  projectIds={v.projects.map((p) => p.project_id)}
                  revision={v.orchestration.revision}
                  onAnswered={applyUpdate}
                />
              {/snippet}
            </Card>
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#if v.pending_approvals.length > 0}
    <section aria-labelledby="approvals-heading">
      <h2 id="approvals-heading">Pending approvals</h2>
      <label class="approver">
        Approved by
        <input type="text" bind:value={approvedBy} placeholder="your name" />
      </label>
      <ul class="approvals">
        {#each v.pending_approvals as manifest (manifest.id)}
          <li>
            <DeliveryApprovalCard {orchestrationId} {manifest} {approvedBy} onResolved={applyUpdate} />
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#if v.blockers.length > 0}
    <section aria-labelledby="blockers-heading">
      <h2 id="blockers-heading">Blockers</h2>
      <ul class="blockers">
        {#each v.blockers as blocker (blocker.lane_id)}
          <li>
            <strong>{blocker.lane_id}</strong>
            {#if blocker.parent_task_id}<span class="meta">task {blocker.parent_task_id}</span>{/if}
            blocked by {blocker.blocked_by.join(", ")}
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  <section aria-labelledby="projects-heading">
    <h2 id="projects-heading">Projects &amp; lanes</h2>
    <div class="projects">
      {#each v.projects as project (project.project_id)}
        <Card>
          {#snippet header()}
            <div class="project-head">
              <span class="project-name">
                <strong>{project.project_id}</strong>
                <!-- Attached means the delivery states it involves this
                     project; anything else is here only because a lane names
                     it, which includes a project detached once its lanes
                     finished. Saying which is which is the difference between
                     "this is in scope" and "work happened here". -->
                {#if project.attached}
                  <span class="attach-badge attached" data-testid={`attached-${project.project_id}`}>Attached</span>
                {:else}
                  <span class="attach-badge detached" data-testid={`unattached-${project.project_id}`}>
                    Not attached · has lanes here
                  </span>
                {/if}
              </span>
              <span class="counts">
                {#each Object.entries(project.counts_by_status) as [status, count] (status)}
                  <StatusBadge
                    variant={laneStatusVariants[status as DeliveryLaneStatus] ?? "neutral"}
                    label={`${count} ${status}`}
                  />
                {/each}
              </span>
            </div>
          {/snippet}
          {#snippet children()}
            <ul class="lanes">
              {#each lanesFor(project.project_id) as lane (lane.lane_id)}
                {@const stage = stageLabel(lane)}
                <li class="lane" class:newly-runnable={newlyRunnable.has(lane.lane_id)}>
                  <div class="lane-head">
                    <span class="lane-id">{lane.lane_id}</span>
                    <StatusBadge variant={laneStatusVariants[lane.status] ?? "neutral"} label={lane.status} />
                    {#if newlyRunnable.has(lane.lane_id)}
                      <span class="new-badge">Newly runnable</span>
                    {/if}
                  </div>
                  {#if lane.parent_task_id}<span class="meta">task {lane.parent_task_id}</span>{/if}
                  {#if lane.blocked_by?.length}
                    <span class="blocked-by">blocked by {lane.blocked_by.join(", ")}</span>
                  {/if}
                  {#if lane.pr_url}
                    <a class="pr-link" href={lane.pr_url} target="_blank" rel="noopener noreferrer">
                      PR{#if lane.pr_number}
                        #{lane.pr_number}
                      {/if}{#if lane.pr_provider}
                        ({lane.pr_provider})
                      {/if}
                    </a>
                  {/if}
                  {#if lane.session_id || lane.worker || lane.worktree_path || lane.base_sha}
                    <div class="lane-detail meta">
                      <!-- The session that opened the lane and the worker
                           currently holding its lease are two different
                           things, so they are labelled separately and never
                           collapsed into one "who" field. -->
                      {#if lane.session_id}<span>session {lane.session_id}</span>{/if}
                      {#if lane.worker}<span>worker {lane.worker}</span>{/if}
                      {#if lane.worktree_path}<span>worktree {lane.worktree_path}</span>{/if}
                      {#if lane.base_sha}<span>base {shortSha(lane.base_sha)}{lane.base_remote ? ` (${lane.base_remote})` : ""}</span>{/if}
                    </div>
                  {/if}
                  {#if stage}
                    <span class="meta">stage: {stage}</span>
                  {/if}
                  {#if lane.repair_cycle_count}
                    <span class="meta">
                      attempt {lane.attempt ?? 1} · {lane.repair_cycle_count} repair cycle{lane.repair_cycle_count === 1 ? "" : "s"}
                    </span>
                  {/if}
                  {#if lane.escalated_at}
                    <span class="escalated">escalated at {lane.escalated_at}</span>
                  {/if}
                  {#if lane.evidence?.length}
                    <ul class="evidence">
                      {#each lane.evidence as ev (ev.id)}
                        <li>
                          <a
                            href={deliveryEvidenceUrl(orchestrationId, ev.id)}
                            target="_blank"
                            rel="noopener noreferrer"
                          >
                            {ev.kind} evidence ({ev.byte_size}B)
                          </a>
                        </li>
                      {/each}
                    </ul>
                  {/if}
                </li>
              {/each}
              {#if lanesFor(project.project_id).length === 0}
                <li class="empty">No lanes yet.</li>
              {/if}
            </ul>
          {/snippet}
        </Card>
      {/each}
    </div>
  </section>
{/if}

<style>
  .error {
    color: var(--color-danger);
  }
  .status-row {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    margin-bottom: 1rem;
    flex-wrap: wrap;
  }
  .meta {
    color: var(--color-text-muted);
    font-size: 0.8rem;
    min-width: 0;
  }
  .meta .id,
  .references .id {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    overflow-wrap: anywhere;
  }
  .description {
    margin: 0 0 1rem;
    max-width: 68ch;
    font-size: 0.9rem;
    line-height: 1.5;
    color: var(--color-text);
    white-space: pre-wrap;
  }
  .references {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 0.25rem 0.7rem;
    margin: 0 0 1rem;
    font-size: 0.8rem;
  }
  .references dt {
    color: var(--color-text-muted);
  }
  .references dd {
    margin: 0;
    min-width: 0;
  }
  .unset {
    color: var(--color-text-muted);
    font-style: italic;
  }
  .project-name {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    flex-wrap: wrap;
    min-width: 0;
  }
  .attach-badge {
    font-size: 0.68rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    border-radius: 999px;
    padding: 0.1rem 0.5rem;
  }
  .attach-badge.attached {
    color: var(--color-accent);
    background: color-mix(in srgb, var(--color-accent) 14%, var(--color-surface));
  }
  .attach-badge.detached {
    color: var(--color-text-muted);
    background: var(--color-surface-subtle);
    text-transform: none;
    letter-spacing: 0;
    font-weight: 600;
  }
  section {
    margin-top: 1.6rem;
  }
  h2 {
    font-size: 1rem;
    margin: 0 0 0.6rem;
  }
  .questions,
  .approvals,
  .blockers {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 0.7rem;
  }
  .blockers li {
    background: var(--color-surface-subtle);
    border-radius: 8px;
    padding: 0.5rem 0.7rem;
    font-size: 0.85rem;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .approver {
    display: inline-grid;
    gap: 0.2rem;
    font-size: 0.8rem;
    color: var(--color-text-muted);
    margin-bottom: 0.8rem;
  }
  .approver input {
    font: inherit;
    padding: 0.35rem 0.5rem;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    background: var(--color-surface);
    color: var(--color-text);
  }
  .approvals {
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  }
  .projects {
    display: grid;
    grid-template-columns: 1fr;
    gap: 1rem;
  }
  @media (min-width: 900px) {
    .projects {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  .project-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    width: 100%;
    flex-wrap: wrap;
  }
  .counts {
    display: flex;
    gap: 0.3rem;
    flex-wrap: wrap;
  }
  .lanes {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 0.5rem;
  }
  .lane {
    border: 1px solid var(--color-border);
    border-radius: 8px;
    padding: 0.5rem 0.65rem;
    display: grid;
    gap: 0.2rem;
    font-size: 0.82rem;
  }
  .lane.newly-runnable {
    border-color: var(--color-success);
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--color-success) 40%, transparent);
  }
  .lane-head {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    flex-wrap: wrap;
  }
  .lane-id {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-weight: 600;
  }
  .new-badge {
    font-size: 0.68rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--color-success);
    background: color-mix(in srgb, var(--color-success) 16%, var(--color-surface));
    border-radius: 999px;
    padding: 0.1rem 0.5rem;
  }
  .blocked-by {
    color: var(--color-warning);
  }
  .pr-link {
    color: var(--color-accent);
    font-weight: 600;
    text-decoration: none;
  }
  .pr-link:hover {
    text-decoration: underline;
  }
  .lane-detail {
    display: flex;
    flex-wrap: wrap;
    gap: 0.6rem;
  }
  .escalated {
    color: var(--color-danger);
    font-weight: 600;
    font-size: 0.78rem;
  }
  .evidence {
    list-style: none;
    padding: 0;
    margin: 0.1rem 0 0;
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }
  .evidence a {
    color: var(--color-accent);
    font-size: 0.78rem;
    text-decoration: none;
  }
  .evidence a:hover {
    text-decoration: underline;
  }
  .empty {
    color: var(--color-text-muted);
    font-size: 0.8rem;
  }
</style>
