<script lang="ts">
  import { onMount } from "svelte";
  import { getSystem, listWorkspaces, type SystemInfo, type WorkspaceSummary } from "./lib/api/client";

  let system: SystemInfo | null = $state(null);
  let workspaces: WorkspaceSummary[] = $state([]);
  let error: string | null = $state(null);
  let loading = $state(true);

  async function load() {
    loading = true;
    error = null;
    try {
      const [sys, ws] = await Promise.all([getSystem(), listWorkspaces()]);
      system = sys;
      workspaces = ws.items;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(load);
</script>

<main>
  <header>
    <h1>Punakawan Panel</h1>
    {#if system}
      <span class="badge" data-testid="read-only-badge">
        {system.read_only ? "Read-only" : "Read-write"}
      </span>
      <span class="version">v{system.panel_version}</span>
    {/if}
  </header>

  {#if loading}
    <p>Loading…</p>
  {:else if error}
    <p role="alert" class="error">Failed to load the panel: {error}</p>
  {:else if workspaces.length === 0}
    <div class="empty">
      <p>No Punakawan workspaces are registered.</p>
      <pre>punakawan workspace register /path/to/project</pre>
    </div>
  {:else}
    <ul class="workspaces" aria-label="Registered workspaces">
      {#each workspaces as ws (ws.id)}
        <li>
          <strong>{ws.display_name || ws.id}</strong>
          <span class="path">{ws.path}</span>
          <span class="availability availability-{ws.availability}">{ws.availability}</span>
          <span class="counts">
            {ws.open_task_count} open · {ws.blocked_task_count} blocked · {ws.active_session_count} active session(s)
          </span>
        </li>
      {/each}
    </ul>
  {/if}
</main>

<style>
  main {
    font-family: system-ui, sans-serif;
    max-width: 960px;
    margin: 0 auto;
    padding: 1rem;
  }
  header {
    display: flex;
    align-items: baseline;
    gap: 0.75rem;
    margin-bottom: 1rem;
  }
  .badge {
    font-size: 0.75rem;
    padding: 0.15rem 0.5rem;
    border-radius: 4px;
    background: #eee;
  }
  .version {
    color: #666;
    font-size: 0.85rem;
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
  ul.workspaces li {
    border: 1px solid #ddd;
    border-radius: 6px;
    padding: 0.75rem 1rem;
    display: grid;
    gap: 0.25rem;
  }
  .path {
    color: #666;
    font-size: 0.85rem;
  }
  .counts {
    font-size: 0.85rem;
  }
</style>
