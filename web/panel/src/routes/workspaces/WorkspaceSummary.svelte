<script lang="ts">
  import { onMount } from "svelte";
  import { getWorkspace, listApprovals, type WorkspaceDetail } from "../../lib/api/client";
  import StatusBadge from "../../lib/components/StatusBadge.svelte";
  import { navigate } from "../../lib/router/router.svelte";
  import { onPanelEvent } from "../../lib/events/sse.svelte";

  interface Props {
    workspaceId: string;
  }
  let { workspaceId }: Props = $props();

  let detail: WorkspaceDetail | null = $state(null);
  let pendingApprovals: number | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(true);

  async function load(id: string) {
    loading = true;
    error = null;
    detail = null;
    pendingApprovals = null;
    try {
      const d = await getWorkspace(id);
      detail = d;
      // Sessions/tasks/knowledge/approvals are only served for the primary
      // workspace; fetching the pending-approval count for any other one
      // would 404, so only do it when this is the primary workspace.
      if (d.primary) {
        const appr = await listApprovals(id, "pending");
        pendingApprovals = appr.items.length;
      }
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

  function drill(sub: string) {
    navigate(`/workspaces/${encodeURIComponent(workspaceId)}/${sub}`);
  }
</script>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <p role="alert" class="error">Failed to load this workspace: {error}</p>
{:else if detail}
  {@const canDrill = detail.primary}
  <header>
    <h1>{detail.display_name || detail.id}</h1>
    <StatusBadge availability={detail.availability} />
  </header>
  <p class="path">{detail.path}</p>

  {#if !canDrill}
    <p class="scope-note" role="note">
      This is not the primary workspace this panel instance is serving. Its sessions, tasks, knowledge, and
      approvals are not available here, so the cards below are read-only summary counts.
    </p>
  {/if}

  {#snippet statCard(value: string | number, label: string, sub: string)}
    {#if canDrill}
      <div
        class="card clickable"
        role="button"
        tabindex="0"
        onclick={() => drill(sub)}
        onkeydown={(e) => e.key === "Enter" && drill(sub)}
      >
        <strong>{value}</strong>
        <span>{label}</span>
      </div>
    {:else}
      <div class="card">
        <strong>{value}</strong>
        <span>{label}</span>
      </div>
    {/if}
  {/snippet}

  <section class="cards" aria-label="Summary">
    {@render statCard(detail.active_session_count, "Active sessions", "sessions")}
    {@render statCard(detail.open_task_count, "Open tasks", "tasks")}
    {@render statCard(detail.blocked_task_count, "Blocked tasks", "tasks")}
    {@render statCard(detail.knowledge_count, "Knowledge records", "knowledge")}
    {@render statCard(canDrill ? (pendingApprovals ?? "—") : "—", "Pending approvals", "approvals")}
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
    color: var(--color-text-muted);
    font-size: 0.85rem;
    margin-top: 0.15rem;
  }
  .scope-note {
    color: var(--color-text-muted);
    font-size: 0.8rem;
    background: var(--color-surface-subtle);
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: 0.5rem 0.7rem;
    margin: 0.5rem 0 0;
  }
  .error {
    color: var(--color-danger);
  }
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
    gap: 0.75rem;
    margin: 1rem 0 1.5rem;
  }
  .card {
    border: 1px solid var(--color-border);
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
    background: var(--color-surface-subtle);
  }
  .card span {
    color: var(--color-text-muted);
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
    border: 1px solid var(--color-border);
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
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
</style>
