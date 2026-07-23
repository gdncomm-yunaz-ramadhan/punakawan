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
  import TableCard from "../../lib/components/cards/TableCard.svelte";
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
    source_failure: "Source failure",
    stale_session: "Stale session",
  };

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
  <BentoGrid>
    <MetricCard label="Active sessions" value={ov.active_sessions.length} size="small" />
    <MetricCard label="Blocked tasks" value={ov.blocked_tasks} size="small" />
    <MetricCard label="Pending approvals" value={ov.pending_approvals.length} size="small" />
    <MetricCard label="Available workspaces" value={ov.available_workspaces} size="small" />

    <StatusCard
      size="wide"
      variant={ov.needs_attention.length === 0 ? "success" : "warning"}
      label={ov.needs_attention.length === 0 ? "Nothing needs attention" : "Needs attention"}
      description={ov.needs_attention.length === 0
        ? "All workspaces are healthy."
        : `${ov.needs_attention.length} item(s) across workspaces.`}
    />
    <TableCard title="Active Now" size="wide" state={ov.active_sessions.length === 0 ? "empty" : "default"} emptyMessage="No active sessions.">
      {#snippet children()}
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
          rows={ov.recent_sessions}
          page={recentSessionsPage}
          pageSize={5}
          onPageChange={(p) => (recentSessionsPage = p)}
          emptyMessage="No sessions yet."
        />
      {/snippet}
    </TableCard>

    <TableCard title="Workspaces" size="full">
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
  </BentoGrid>
{/if}

<style>
  .muted {
    color: var(--color-text-muted);
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
    display: flex;
    align-items: center;
    gap: 0.5rem;
    justify-content: space-between;
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
</style>
