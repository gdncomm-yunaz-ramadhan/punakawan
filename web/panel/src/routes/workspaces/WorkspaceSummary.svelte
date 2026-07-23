<script lang="ts">
  import { onMount } from "svelte";
  import { getWorkspace, type WorkspaceDetail } from "../../lib/api/client";
  import StatusBadge from "../../lib/components/StatusBadge.svelte";
  import { navigate } from "../../lib/router/router.svelte";
  import { onPanelEvent } from "../../lib/events/sse.svelte";

  interface Props {
    workspaceId: string;
  }
  let { workspaceId }: Props = $props();

  let detail: WorkspaceDetail | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(true);

  async function load(id: string) {
    loading = true;
    error = null;
    detail = null;
    try {
      detail = await getWorkspace(id);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    load(workspaceId);
    return onPanelEvent(() => load(workspaceId));
  });
  $effect(() => {
    load(workspaceId);
  });
</script>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <p role="alert" class="error">Failed to load this workspace: {error}</p>
{:else if detail}
  <header>
    <h1>{detail.display_name || detail.id}</h1>
    <StatusBadge availability={detail.availability} />
  </header>
  <p class="path">{detail.path}</p>

  <section class="cards" aria-label="Summary">
    <div
      class="card clickable"
      role="button"
      tabindex="0"
      onclick={() => navigate(`/workspaces/${encodeURIComponent(workspaceId)}/sessions`)}
      onkeydown={(e) => e.key === "Enter" && navigate(`/workspaces/${encodeURIComponent(workspaceId)}/sessions`)}
    >
      <strong>{detail.active_session_count}</strong>
      <span>Active sessions</span>
    </div>
    <div
      class="card clickable"
      role="button"
      tabindex="0"
      onclick={() => navigate(`/workspaces/${encodeURIComponent(workspaceId)}/tasks`)}
      onkeydown={(e) => e.key === "Enter" && navigate(`/workspaces/${encodeURIComponent(workspaceId)}/tasks`)}
    >
      <strong>{detail.open_task_count}</strong>
      <span>Open tasks</span>
    </div>
    <div
      class="card clickable"
      role="button"
      tabindex="0"
      onclick={() => navigate(`/workspaces/${encodeURIComponent(workspaceId)}/tasks`)}
      onkeydown={(e) => e.key === "Enter" && navigate(`/workspaces/${encodeURIComponent(workspaceId)}/tasks`)}
    >
      <strong>{detail.blocked_task_count}</strong>
      <span>Blocked tasks</span>
    </div>
    <div class="card">
      <strong>{detail.knowledge_count}</strong>
      <span>Knowledge records</span>
    </div>
  </section>

  <section aria-labelledby="source-health-heading">
    <h2 id="source-health-heading">Source Health</h2>
    <ul class="health">
      {#each detail.health as h (h.source)}
        <li>
          <span class="source">{h.source}</span>
          <StatusBadge availability={h.availability} />
          {#if h.message}<span class="message">{h.message}</span>{/if}
        </li>
      {/each}
    </ul>
  </section>
{/if}

<style>
  header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  h1 {
    font-size: 1.2rem;
    margin: 0;
  }
  .path {
    color: #666;
    font-size: 0.85rem;
    margin-top: 0.15rem;
  }
  .error {
    color: #b00020;
  }
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
    gap: 0.75rem;
    margin: 1rem 0 1.5rem;
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
  .card.clickable {
    cursor: pointer;
  }
  .card.clickable:hover {
    background: #f7f7f7;
  }
  .card span {
    color: #666;
    font-size: 0.85rem;
  }
  h2 {
    font-size: 1rem;
  }
  ul.health {
    list-style: none;
    padding: 0;
    display: grid;
    gap: 0.4rem;
  }
  ul.health li {
    border: 1px solid #eee;
    border-radius: 6px;
    padding: 0.5rem 0.75rem;
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .source {
    font-weight: 600;
    min-width: 100px;
  }
  .message {
    color: #666;
    font-size: 0.85rem;
  }
</style>
