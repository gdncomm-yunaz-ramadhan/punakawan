<script lang="ts">
  import { onMount } from "svelte";
  import { listSessions, type PanelSessionSummary } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import { onPanelEvent } from "../../lib/events/sse.svelte";
  import DataTable from "../../lib/components/data/DataTable.svelte";
  import type { Column, RowAction } from "../../lib/components/data/types";

  interface Props {
    workspaceId: string;
  }
  let { workspaceId }: Props = $props();

  let sessions: PanelSessionSummary[] = $state([]);
  let error: string | null = $state(null);
  let loading = $state(true);
  let page = $state(1);
  let pageSize = $state(10);

  async function load(id: string) {
    loading = true;
    error = null;
    try {
      const res = await listSessions(id);
      sessions = res.items ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    load(workspaceId);
    // Per §12: a live event alone is not a complete object, so refetch
    // the list rather than trying to patch it from the event payload.
    return onPanelEvent(() => load(workspaceId));
  });
  $effect(() => {
    load(workspaceId);
  });

  const columns: Column<PanelSessionSummary>[] = [
    { key: "id", label: "Session", primary: true },
    { key: "workflow", label: "Workflow", sortable: true },
    { key: "status", label: "Status", sortable: true },
    { key: "active_role", label: "Role", render: (s) => s.active_role ?? "—" },
    { key: "started_at", label: "Started", sortable: true, render: (s) => new Date(s.started_at).toLocaleString() },
    { key: "updated_at", label: "Updated", sortable: true, render: (s) => new Date(s.updated_at).toLocaleString() },
  ];

  const rowAction: RowAction<PanelSessionSummary> = {
    label: "Open",
    onSelect: (s) => navigate(`/workspaces/${encodeURIComponent(workspaceId)}/sessions/${encodeURIComponent(s.id)}`),
  };
</script>

{#if error}
  <p role="alert" class="error">Failed to load sessions: {error}</p>
{:else}
  <!--
    Migrated (UI-011/UI-017) to the shared DataTable instead of a bespoke
    <table>: gains sorting, pagination, sticky header, and a mobile
    card-list fallback, and stays visually consistent with the Overview
    and Tasks tables (and theme-aware in both light and dark).
  -->
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
{/if}

<style>
  .error {
    color: var(--color-danger);
  }
</style>
