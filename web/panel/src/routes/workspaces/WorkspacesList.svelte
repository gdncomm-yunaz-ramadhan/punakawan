<script lang="ts">
  import { onMount } from "svelte";
  import { listWorkspaces, type WorkspaceSummary } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import StatusBadge from "../../lib/components/StatusBadge.svelte";

  let workspaces: WorkspaceSummary[] = $state([]);
  let error: string | null = $state(null);
  let loading = $state(true);

  async function load() {
    loading = true;
    error = null;
    try {
      workspaces = (await listWorkspaces()).items;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(load);
</script>

<h1>Workspaces</h1>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <p role="alert" class="error">Failed to load workspaces: {error}</p>
{:else if workspaces.length === 0}
  <div class="empty">
    <p>No Punakawan workspaces are registered.</p>
    <pre>punakawan workspace register /path/to/project</pre>
  </div>
{:else}
  <ul class="workspaces" aria-label="Registered workspaces">
    {#each workspaces as ws (ws.id)}
      <li>
        <button type="button" class="card" onclick={() => navigate(`/workspaces/${encodeURIComponent(ws.id)}`)}>
          <div class="row">
            <strong>{ws.display_name || ws.id}</strong>
            {#if ws.pinned}<span title="Pinned" aria-label="Pinned">📌</span>{/if}
          </div>
          <span class="path">{ws.path}</span>
          <div class="row">
            <StatusBadge availability={ws.availability} />
            <span class="counts">
              {ws.open_task_count} open · {ws.blocked_task_count} blocked · {ws.active_session_count} active session(s)
            </span>
          </div>
        </button>
      </li>
    {/each}
  </ul>
{/if}

<style>
  h1 {
    font-size: 1.2rem;
  }
  .error {
    color: #b00020;
  }
  ul.workspaces {
    list-style: none;
    padding: 0;
    display: grid;
    gap: 0.75rem;
  }
  .card {
    width: 100%;
    text-align: left;
    border: 1px solid #ddd;
    border-radius: 6px;
    padding: 0.75rem 1rem;
    display: grid;
    gap: 0.25rem;
    background: white;
    cursor: pointer;
    font: inherit;
  }
  .card:hover {
    border-color: #3949ab;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    justify-content: space-between;
  }
  .path {
    color: #666;
    font-size: 0.85rem;
  }
  .counts {
    font-size: 0.85rem;
    color: #444;
  }
  .empty pre {
    background: #f5f5f5;
    padding: 0.5rem 0.75rem;
    border-radius: 6px;
    display: inline-block;
  }
</style>
