<script lang="ts">
  import { onMount } from "svelte";
  import { listSessions, type PanelSessionSummary } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import { onPanelEvent } from "../../lib/events/sse.svelte";

  interface Props {
    workspaceId: string;
  }
  let { workspaceId }: Props = $props();

  let sessions: PanelSessionSummary[] = $state([]);
  let error: string | null = $state(null);
  let loading = $state(true);

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
</script>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <p role="alert" class="error">Failed to load sessions: {error}</p>
{:else if sessions.length === 0}
  <p>No sessions yet.</p>
{:else}
  <table>
    <thead>
      <tr>
        <th>Session</th>
        <th>Workflow</th>
        <th>Status</th>
        <th>Role</th>
        <th>Started</th>
        <th>Updated</th>
      </tr>
    </thead>
    <tbody>
      {#each sessions as s (s.id)}
        <tr
          class="row"
          onclick={() => navigate(`/workspaces/${encodeURIComponent(workspaceId)}/sessions/${encodeURIComponent(s.id)}`)}
        >
          <td>{s.id}</td>
          <td>{s.workflow}</td>
          <td><span class="status status-{s.status}">{s.status}</span></td>
          <td>{s.active_role ?? "—"}</td>
          <td>{new Date(s.started_at).toLocaleString()}</td>
          <td>{new Date(s.updated_at).toLocaleString()}</td>
        </tr>
      {/each}
    </tbody>
  </table>
{/if}

<style>
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.9rem;
  }
  th {
    text-align: left;
    color: #666;
    font-weight: 500;
    font-size: 0.8rem;
    padding: 0.4rem 0.6rem;
    border-bottom: 1px solid #ddd;
  }
  td {
    padding: 0.5rem 0.6rem;
    border-bottom: 1px solid #eee;
  }
  .row {
    cursor: pointer;
  }
  .row:hover {
    background: #f7f7f7;
  }
  .status {
    font-size: 0.8rem;
    padding: 0.1rem 0.4rem;
    border-radius: 4px;
    background: #eee;
  }
  .error {
    color: #b00020;
  }
</style>
