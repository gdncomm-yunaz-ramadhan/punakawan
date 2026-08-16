<script lang="ts">
  import { onMount } from "svelte";
  import {
    getDeliveryView,
    cancelDelivery,
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
    load(orchestrationId);
    return onPanelEvent((evt) => {
      if (evt.type !== "delivery.updated") return;
      const parsed = parsePanelEvent(evt);
      if (parsed?.entity_id !== orchestrationId) return;
      refresh(orchestrationId);
    });
  });

  $effect(() => {
    load(orchestrationId);
  });

  function applyUpdate(v: DeliveryView) {
    view = v;
    lastSeq = v.latest_seq;
  }

  async function cancel() {
    if (!view) return;
    cancelling = true;
    cancelError = null;
    try {
      applyUpdate(await cancelDelivery(orchestrationId, { expected_revision: view.orchestration.revision }));
    } catch (e) {
      cancelError = e instanceof Error ? e.message : String(e);
    } finally {
      cancelling = false;
    }
  }

  function lanesFor(projectId: string): DeliveryLaneSummary[] {
    return view?.lanes.filter((l) => l.project_id === projectId) ?? [];
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
  <PageHeader title={orchestrationId} description={v.next_action}>
    {#snippet actions()}
      {#if v.orchestration.status === "pending" || v.orchestration.status === "active"}
        <Button variant="danger" size="sm" disabled={cancelling} onclick={cancel}>
          {cancelling ? "Cancelling…" : "Cancel delivery"}
        </Button>
      {/if}
    {/snippet}
  </PageHeader>

  {#if cancelError}
    <p role="alert" class="error">{cancelError}</p>
  {/if}

  <div class="status-row">
    <StatusBadge
      variant={orchestrationStatusVariants[v.orchestration.status] ?? "neutral"}
      label={v.orchestration.status}
    />
    <span class="meta">revision {v.orchestration.revision} · latest seq {v.latest_seq}</span>
  </div>

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
              <strong>{project.project_id}</strong>
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
  }
  .meta {
    color: var(--color-text-muted);
    font-size: 0.8rem;
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
  .empty {
    color: var(--color-text-muted);
    font-size: 0.8rem;
  }
</style>
