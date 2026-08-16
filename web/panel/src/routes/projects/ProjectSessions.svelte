<script lang="ts">
  import { onMount } from "svelte";
  import {
    listProjectSessions,
    getProjectSession,
    listProjectEvidence,
    type PanelSessionSummary,
    type SessionDetail,
    type TimelineEvent,
    type EvidenceRecord,
  } from "../../lib/api/client";
  import { onPanelEvent } from "../../lib/events/sse.svelte";
  import { roleLabel } from "../../lib/roles";
  import DataTable from "../../lib/components/data/DataTable.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import EvidenceItem from "../../lib/components/EvidenceItem.svelte";
  import type { Column, RowAction } from "../../lib/components/data/types";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  let sessions: PanelSessionSummary[] = $state([]);
  let loading = $state(true);
  let error: string | null = $state(null);
  let page = $state(1);
  let pageSize = $state(10);

  let selectedId: string | null = $state(null);
  let detail: SessionDetail | null = $state(null);
  let detailLoading = $state(false);
  let detailError: string | null = $state(null);
  let evidence: EvidenceRecord[] = $state([]);

  async function load(id: string) {
    loading = true;
    error = null;
    try {
      const res = await listProjectSessions(id);
      sessions = res.items ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function open(sessionId: string) {
    if (selectedId === sessionId) {
      selectedId = null;
      detail = null;
      evidence = [];
      return;
    }
    selectedId = sessionId;
    detail = null;
    evidence = [];
    detailError = null;
    detailLoading = true;
    try {
      const [d, ev] = await Promise.all([
        getProjectSession(projectId, sessionId),
        listProjectEvidence(projectId, sessionId),
      ]);
      detail = d;
      evidence = ev.items;
    } catch (e) {
      detailError = e instanceof Error ? e.message : String(e);
    } finally {
      detailLoading = false;
    }
  }

  onMount(() => {
    load(projectId);
    return onPanelEvent(() => load(projectId));
  });
  $effect(() => {
    load(projectId);
  });

  const columns: Column<PanelSessionSummary>[] = [
    { key: "id", label: "Session", primary: true },
    { key: "workflow", label: "Workflow", sortable: true },
    { key: "status", label: "Status", sortable: true },
    { key: "active_role", label: "Role", render: (s) => roleLabel(s.active_role) },
    { key: "updated_at", label: "Updated", sortable: true, render: (s) => new Date(s.updated_at).toLocaleString() },
  ];

  const rowAction: RowAction<PanelSessionSummary> = {
    label: "Open",
    onSelect: (s) => open(s.id),
  };

  function isFailure(e: TimelineEvent): boolean {
    return e.result === "failure" || e.result === "timeout" || e.result === "cancelled";
  }
</script>

<section aria-label="Project sessions">
  {#if error}
    <ErrorStateCard title="Failed to load sessions" message={error} />
  {:else if !loading && sessions.length === 0}
    <EmptyStateCard title="No sessions yet" message="Runs started against this project will appear here." />
  {:else}
    <DataTable
      {columns}
      rows={sessions}
      {loading}
      {page}
      {pageSize}
      onPageChange={(p) => (page = p)}
      onPageSizeChange={(s) => {
        pageSize = s;
        page = 1;
      }}
      {rowAction}
      emptyMessage="No sessions yet."
    />

    {#if selectedId}
      <div class="detail" data-testid="session-detail">
        {#if detailLoading}
          <p>Loading session…</p>
        {:else if detailError}
          <ErrorStateCard title="Failed to load session" message={detailError} />
        {:else if detail}
          <header class="detail-head">
            <h3>{detail.id}</h3>
            <span class="status">{detail.status}</span>
          </header>
          <p class="meta">
            {detail.workflow}
            {#if detail.initiator}· initiated by {detail.initiator}{/if}
            {#if detail.active_role}· active role: {roleLabel(detail.active_role)}{/if}
          </p>
          {#if detail.objective}<p class="objective">{detail.objective}</p>{/if}

          <div class="counts">
            <div class="count-card"><strong>{detail.task_counts?.total ?? 0}</strong><span>Tasks</span></div>
            <div class="count-card"><strong>{detail.task_counts?.open ?? 0}</strong><span>Open</span></div>
            <div class="count-card"><strong>{detail.task_counts?.in_progress ?? 0}</strong><span>In progress</span></div>
            <div class="count-card"><strong>{detail.task_counts?.blocked ?? 0}</strong><span>Blocked</span></div>
            <div class="count-card"><strong>{detail.task_counts?.closed ?? 0}</strong><span>Closed</span></div>
            <div class="count-card"><strong>{detail.evidence_count ?? 0}</strong><span>Evidence</span></div>
          </div>

          <h4>Evidence</h4>
          {#if evidence.length === 0}
            <p class="muted">No evidence recorded for this session.</p>
          {:else}
            <ul class="evidence">
              {#each evidence as rec (rec.id)}
                <EvidenceItem {projectId} record={rec} />
              {/each}
            </ul>
          {/if}

          <h4>Phase timeline</h4>
          {#if !detail.Timeline || detail.Timeline.length === 0}
            <p class="muted">No events recorded yet.</p>
          {:else}
            <ol class="timeline">
              {#each detail.Timeline as e (e.id)}
                <li class:failure={isFailure(e)}>
                  <span class="time">{new Date(e.timestamp).toLocaleTimeString()}</span>
                  <span class="op">{e.operation}</span>
                  {#if e.role}<span class="role">{roleLabel(e.role)}</span>{/if}
                  <span class="result result-{e.result}">{e.result}</span>
                </li>
              {/each}
            </ol>
          {/if}
        {/if}
      </div>
    {/if}
  {/if}
</section>

<style>
  .detail {
    margin-top: 1rem;
    padding: 1rem 1.1rem;
    background: var(--color-surface-subtle);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
  }
  .detail-head {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    flex-wrap: wrap;
  }
  .detail-head h3 {
    margin: 0;
    font-size: 1.05rem;
    word-break: break-all;
  }
  .status {
    font-size: 0.78rem;
    padding: 0.1rem 0.45rem;
    border-radius: 4px;
    background: var(--color-surface);
    text-transform: capitalize;
  }
  .meta {
    color: var(--color-text-muted);
    font-size: 0.85rem;
    margin: 0.15rem 0;
  }
  .objective {
    font-size: 0.95rem;
    margin: 0.2rem 0 0;
  }
  .counts {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(96px, 1fr));
    gap: 0.6rem;
    margin: 0.9rem 0 1rem;
  }
  .count-card {
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
    padding: 0.55rem 0.7rem;
    background: var(--color-surface);
    display: grid;
    gap: 0.1rem;
  }
  .count-card strong {
    font-size: 1.25rem;
    font-variant-numeric: tabular-nums;
  }
  .count-card span {
    color: var(--color-text-muted);
    font-size: 0.78rem;
  }
  h4 {
    margin: 0 0 0.35rem;
    font-size: 0.9rem;
  }
  .muted {
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
  ul.evidence {
    list-style: none;
    padding: 0;
    margin: 0 0 1rem;
    display: grid;
    gap: 0.4rem;
  }
  ol.timeline {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 0.3rem;
  }
  ol.timeline li {
    display: flex;
    gap: 0.6rem;
    align-items: center;
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
    padding: 0.4rem 0.6rem;
    font-size: 0.85rem;
    background: var(--color-surface);
  }
  ol.timeline li.failure {
    border-color: var(--color-danger);
    background: var(--color-accent-soft);
  }
  .time {
    color: var(--color-text-muted);
    font-size: 0.8rem;
    min-width: 5.5rem;
  }
  .op {
    flex: 1;
  }
  .role {
    color: var(--color-accent);
    font-size: 0.8rem;
    text-transform: capitalize;
  }
  .result {
    font-size: 0.75rem;
    padding: 0.05rem 0.4rem;
    border-radius: 4px;
    background: var(--color-surface-subtle);
  }
  .result-failure,
  .result-timeout,
  .result-cancelled {
    background: var(--color-accent-soft);
    color: var(--color-danger);
  }
  .result-success {
    background: var(--color-accent-soft);
    color: var(--color-success);
  }
</style>
