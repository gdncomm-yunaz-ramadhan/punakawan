<script lang="ts">
  import { onMount } from "svelte";
  import { getOverview, type Overview, type PanelSessionSummary } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import StatusBadge from "../../lib/components/StatusBadge.svelte";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import { onPanelEvent } from "../../lib/events/sse.svelte";
  import BentoGrid from "../../lib/components/cards/BentoGrid.svelte";
  import MetricCard from "../../lib/components/cards/MetricCard.svelte";
  import StatusCard from "../../lib/components/cards/StatusCard.svelte";
  import ChartCard from "../../lib/components/cards/ChartCard.svelte";
  import TableCard from "../../lib/components/cards/TableCard.svelte";
  import BlockedTasksChart from "../../lib/components/charts/BlockedTasksChart.svelte";
  import DataTable from "../../lib/components/data/DataTable.svelte";
  import type { Column } from "../../lib/components/data/types";

  let overview: Overview | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(true);
  let recentSessionsPage = $state(1);

  async function load() {
    loading = true;
    error = null;
    try {
      overview = await getOverview();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    load();
    return onPanelEvent(load);
  });

  const attentionLabels: Record<string, string> = {
    failed_session: "Failed session",
    pending_approval: "Pending approval",
    blocked_tasks: "Blocked tasks",
    unavailable_workspace: "Unavailable workspace",
    stale_session: "Stale session",
  };

  // Recent sessions are shown newest-first (updated_at desc) so the table's
  // default order is deterministic and actually recent. The backend already
  // sorts them this way; this keeps the guarantee if that ever changes.
  function sortRecent(list: PanelSessionSummary[]): PanelSessionSummary[] {
    return [...list].sort((a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime());
  }

  const recentSessionColumns: Column<PanelSessionSummary>[] = [
    { key: "objective", label: "Objective", primary: true, render: (s) => s.objective || s.id },
    { key: "workflow", label: "Workflow", sortable: true },
    { key: "status", label: "Status", sortable: true },
    {
      key: "updated_at",
      label: "Updated",
      sortable: true,
      render: (s) => new Date(s.updated_at).toLocaleString(),
    },
  ];
</script>

<PageHeader title="Overview" description="Everything currently active or needing attention across workspaces." />

{#if loading}
  <p>Loading…</p>
{:else if error}
  <p role="alert" class="error">Failed to load the overview: {error}</p>
{:else if overview}
  {@const ov = overview}
  {@const blockedByWorkspace = ov.workspace_health
    .filter((w) => w.blocked_task_count > 0)
    .sort((a, b) => b.blocked_task_count - a.blocked_task_count)}
  <BentoGrid>
    <!-- span 3 on the 12/6/1-col BentoGrid => 4 per row (desktop),
         2 per row (tablet), 1 per row (mobile): the 4/2/1 cap. -->
    <MetricCard label="Active sessions" value={ov.active_sessions.length} size="small" columns={3} accent="indigo" icon="activity" />
    <MetricCard label="Blocked tasks" value={ov.blocked_tasks} size="small" columns={3} accent="terracotta" icon="alert" />
    <MetricCard label="Pending approvals" value={ov.pending_approvals.length} size="small" columns={3} accent="gold" icon="approval" />
    <MetricCard label="Available workspaces" value={ov.available_workspaces} size="small" columns={3} accent="teal" icon="workspace" />

    <ChartCard
      title="Blocked tasks by workspace"
      description="Where blocked work is concentrated right now."
      size="wide"
      state={blockedByWorkspace.length === 0 ? "empty" : "default"}
      emptyMessage="No blocked tasks across workspaces."
    >
      {#snippet children()}
        <div class="chart-fill">
          <BlockedTasksChart
            items={blockedByWorkspace.map((w) => ({ label: w.display_name || w.id, value: w.blocked_task_count }))}
          />
        </div>
      {/snippet}
    </ChartCard>

    <StatusCard
      size="medium"
      variant={ov.needs_attention.length === 0 ? "success" : "warning"}
      label={ov.needs_attention.length === 0 ? "Nothing needs attention" : "Needs attention"}
      description={ov.needs_attention.length === 0
        ? "All workspaces are healthy."
        : `${ov.needs_attention.length} item(s) across workspaces.`}
    >
      {#snippet children()}
        {#if ov.needs_attention.length > 0}
          <!-- Preview the top few items so the card carries real content
               rather than a lone count; the full list is the "Needs
               Attention" card below. -->
          <ul class="attention-preview">
            {#each ov.needs_attention.slice(0, 3) as item, i (i)}
              <li>
                <span class="kind">{attentionLabels[item.kind] ?? item.kind}</span>
                <span class="where">{item.workspace_id}</span>
              </li>
            {/each}
          </ul>
          {#if ov.needs_attention.length > 3}
            <span class="attention-more">…{ov.needs_attention.length - 3} more below</span>
          {/if}
        {/if}
      {/snippet}
    </StatusCard>

    <TableCard title="Active Now" size="wide" state={ov.active_sessions.length === 0 ? "empty" : "default"} emptyMessage="No active sessions.">
      {#snippet children()}
        {#if ov.primary_workspace_id}
          <p class="scope-note">Sessions and approvals below are scoped to the primary workspace ({ov.primary_workspace_id}).</p>
        {/if}
        <ul class="sessions">
          {#each ov.active_sessions as s (s.id)}
            <li>
              <strong>{s.objective || s.id}</strong>
              <span class="muted">{s.workflow} · {s.status}{s.active_role ? ` · ${s.active_role}` : ""}</span>
            </li>
          {/each}
        </ul>
      {/snippet}
    </TableCard>

    <TableCard title="Workspaces" size="medium" state={ov.workspace_health.length === 0 ? "empty" : "default"} emptyMessage="No workspaces registered.">
      {#snippet children()}
        <ul class="workspaces">
          {#each ov.workspace_health as ws (ws.id)}
            <li>
              <button
                type="button"
                class="link-button"
                onclick={() => navigate(`/workspaces/${encodeURIComponent(ws.id)}`)}
              >
                {ws.display_name || ws.id}
              </button>
              <StatusBadge availability={ws.availability} />
            </li>
          {/each}
        </ul>
      {/snippet}
    </TableCard>

    {#if ov.needs_attention.length > 0}
      <TableCard title="Needs Attention" size="full">
        {#snippet children()}
          <ol class="attention">
            {#each ov.needs_attention as item, i (i)}
              <li>
                <span class="kind">{attentionLabels[item.kind] ?? item.kind}</span>
                <span>{item.message}</span>
                <button
                  type="button"
                  class="link-button"
                  onclick={() => navigate(`/workspaces/${encodeURIComponent(item.workspace_id)}`)}
                >
                  {item.workspace_id}
                </button>
              </li>
            {/each}
          </ol>
        {/snippet}
      </TableCard>
    {/if}

    <TableCard title="Recent Sessions" size="full">
      {#snippet children()}
        <DataTable
          columns={recentSessionColumns}
          rows={sortRecent(ov.recent_sessions)}
          page={recentSessionsPage}
          pageSize={5}
          onPageChange={(p) => (recentSessionsPage = p)}
          emptyMessage="No sessions yet."
        />
      {/snippet}
    </TableCard>
  </BentoGrid>
{/if}

<style>
  .muted {
    color: var(--color-text-muted);
  }
  .scope-note {
    color: var(--color-text-muted);
    font-size: 0.75rem;
    margin: 0 0 0.5rem;
  }
  .error {
    color: var(--color-danger);
  }
  ul.sessions,
  ol.attention,
  ul.workspaces {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 0.4rem;
  }
  ul.sessions li,
  ol.attention li,
  ul.workspaces li {
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: 0.5rem 0.75rem;
    min-height: 40px;
    display: flex;
    align-items: center;
    /* Wrap onto a second line on narrow widths instead of forcing the
       body to scroll horizontally; long ids/objectives break rather
       than overflow. */
    flex-wrap: wrap;
    gap: 0.5rem;
    justify-content: space-between;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  ul.sessions li > *,
  ol.attention li > * {
    min-width: 0;
  }
  .kind {
    font-weight: 600;
    font-size: 0.85rem;
    color: var(--color-text);
  }
  .link-button {
    background: none;
    border: none;
    padding: 0;
    color: var(--color-accent);
    cursor: pointer;
    font-size: inherit;
    text-decoration: underline;
  }

  /* Compact preview inside the "Needs attention" summary card. */
  ul.attention-preview {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 0.3rem;
  }
  ul.attention-preview li {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.5rem;
    font-size: 0.8rem;
    min-width: 0;
  }
  ul.attention-preview .kind {
    font-size: 0.8rem;
  }
  ul.attention-preview .where {
    color: var(--color-text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 55%;
  }
  .attention-more {
    display: block;
    margin-top: 0.4rem;
    color: var(--color-text-muted);
    font-size: 0.75rem;
  }

  /* Let the chart fill the card's content box so it uses the full tile
     width rather than sitting in a fixed-width island. */
  .chart-fill {
    width: 100%;
    min-width: 0;
  }
</style>
