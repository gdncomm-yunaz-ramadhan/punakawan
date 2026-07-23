<script lang="ts">
  import { onMount } from "svelte";
  import { getOverview, type Overview } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import StatusBadge from "../../lib/components/StatusBadge.svelte";

  let overview: Overview | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(true);

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

  onMount(load);

  const attentionLabels: Record<string, string> = {
    failed_session: "Failed session",
    pending_approval: "Pending approval",
    blocked_tasks: "Blocked tasks",
    unavailable_workspace: "Unavailable workspace",
    source_failure: "Source failure",
    stale_session: "Stale session",
  };
</script>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <p role="alert" class="error">Failed to load the overview: {error}</p>
{:else if overview}
  <section class="cards" aria-label="Summary">
    <div class="card">
      <strong>{overview.active_sessions.length}</strong>
      <span>Active sessions</span>
    </div>
    <div class="card">
      <strong>{overview.blocked_tasks}</strong>
      <span>Blocked tasks</span>
    </div>
    <div class="card">
      <strong>{overview.pending_approvals.length}</strong>
      <span>Pending approvals</span>
    </div>
    <div class="card">
      <strong>{overview.available_workspaces}</strong>
      <span>Available workspaces</span>
    </div>
  </section>

  <section aria-labelledby="active-now-heading">
    <h2 id="active-now-heading">Active Now</h2>
    {#if overview.active_sessions.length === 0}
      <p class="muted">No active sessions.</p>
    {:else}
      <ul class="sessions">
        {#each overview.active_sessions as s (s.id)}
          <li>
            <strong>{s.objective || s.id}</strong>
            <span class="muted">{s.workflow} · {s.status}{s.active_role ? ` · ${s.active_role}` : ""}</span>
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <section aria-labelledby="needs-attention-heading">
    <h2 id="needs-attention-heading">Needs Attention</h2>
    {#if overview.needs_attention.length === 0}
      <p class="muted">Nothing needs attention.</p>
    {:else}
      <ol class="attention">
        {#each overview.needs_attention as item, i (i)}
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
    {/if}
  </section>

  <section aria-labelledby="recent-sessions-heading">
    <h2 id="recent-sessions-heading">Recent Sessions</h2>
    {#if overview.recent_sessions.length === 0}
      <p class="muted">No sessions yet.</p>
    {:else}
      <table>
        <thead>
          <tr>
            <th scope="col">Objective</th>
            <th scope="col">Workflow</th>
            <th scope="col">Status</th>
            <th scope="col">Updated</th>
          </tr>
        </thead>
        <tbody>
          {#each overview.recent_sessions as s (s.id)}
            <tr>
              <td>{s.objective || s.id}</td>
              <td>{s.workflow}</td>
              <td>{s.status}</td>
              <td>{new Date(s.updated_at).toLocaleString()}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </section>

  <section aria-labelledby="workspace-health-heading">
    <h2 id="workspace-health-heading">Workspaces</h2>
    <ul class="workspaces">
      {#each overview.workspace_health as ws (ws.id)}
        <li>
          <button type="button" class="link-button" onclick={() => navigate(`/workspaces/${encodeURIComponent(ws.id)}`)}>
            {ws.display_name || ws.id}
          </button>
          <StatusBadge availability={ws.availability} />
        </li>
      {/each}
    </ul>
  </section>
{/if}

<style>
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
    gap: 0.75rem;
    margin-bottom: 1.5rem;
  }
  .card {
    border: 1px solid #ddd;
    border-radius: 6px;
    padding: 0.75rem 1rem;
    display: grid;
    gap: 0.15rem;
  }
  .card strong {
    font-size: 1.5rem;
  }
  .card span {
    color: #666;
    font-size: 0.85rem;
  }
  section {
    margin-bottom: 1.5rem;
  }
  h2 {
    font-size: 1rem;
    margin-bottom: 0.5rem;
  }
  .muted {
    color: #666;
  }
  .error {
    color: #b00020;
  }
  ul.sessions,
  ol.attention,
  ul.workspaces {
    list-style: none;
    padding: 0;
    display: grid;
    gap: 0.4rem;
  }
  ul.sessions li,
  ol.attention li,
  ul.workspaces li {
    border: 1px solid #eee;
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
  }
  table {
    width: 100%;
    border-collapse: collapse;
  }
  th,
  td {
    text-align: left;
    padding: 0.4rem 0.5rem;
    border-bottom: 1px solid #eee;
    font-size: 0.9rem;
  }
  .link-button {
    background: none;
    border: none;
    padding: 0;
    color: #3949ab;
    cursor: pointer;
    font-size: inherit;
    text-decoration: underline;
  }

  @media (max-width: 640px) {
    .cards {
      grid-template-columns: 1fr 1fr;
    }
  }
</style>
