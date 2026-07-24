<script lang="ts">
  import { onMount } from "svelte";
  import { listProjects, type ProjectSummary } from "../../lib/api/client";
  import type { Availability } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import StatusBadge from "../../lib/components/StatusBadge.svelte";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import { onPanelEvent } from "../../lib/events/sse.svelte";

  let projects: ProjectSummary[] = $state([]);
  let error: string | null = $state(null);
  let loading = $state(true);

  async function load() {
    loading = true;
    error = null;
    try {
      projects = (await listProjects()).items;
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

  function open(id: string) {
    navigate(`/projects/${encodeURIComponent(id)}`);
  }
</script>

<PageHeader title="Projects" description="Every workspace your Punakawan instance tracks, with its live snapshot counts." />

{#if loading}
  <p>Loading…</p>
{:else if error}
  <ErrorStateCard title="Failed to load projects" message={error} />
{:else if projects.length === 0}
  <EmptyStateCard
    title="No projects yet"
    message="Register a project with the Punakawan CLI to see it here."
  />
{:else}
  <ul class="projects" aria-label="Projects">
    {#each projects as p (p.id)}
      <li>
        <button type="button" class="card" onclick={() => open(p.id)}>
          <div class="row">
            <strong class="name">{p.name || p.id}</strong>
            <span class="markers">
              {#if p.primary}<span class="tag primary" title="Primary project">primary</span>{/if}
              {#if p.pinned}<span title="Pinned" aria-label="Pinned">📌</span>{/if}
            </span>
          </div>
          {#if p.description}<span class="description">{p.description}</span>{/if}
          <span class="path">{p.path}</span>
          <div class="row wrap">
            <StatusBadge availability={p.availability as Availability} />
            <span class="counts">
              {p.repository_count} repos · {p.open_task_count} open · {p.blocked_task_count} blocked ·
              {p.active_session_count} active · {p.knowledge_count} knowledge · {p.metadata_count} metadata
            </span>
          </div>
        </button>
      </li>
    {/each}
  </ul>
{/if}

<style>
  ul.projects {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 0.75rem;
  }
  .card {
    width: 100%;
    text-align: left;
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 0.85rem 1.1rem;
    display: grid;
    gap: 0.3rem;
    background: var(--color-surface);
    cursor: pointer;
    font: inherit;
    color: var(--color-text);
  }
  .card:hover {
    border-color: var(--color-accent);
  }
  .card:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    justify-content: space-between;
  }
  .row.wrap {
    flex-wrap: wrap;
    justify-content: flex-start;
  }
  .name {
    font-size: 1.02rem;
  }
  .markers {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
  }
  .tag.primary {
    font-size: 0.7rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--color-accent);
    background: var(--color-accent-soft);
    border-radius: 999px;
    padding: 0.1rem 0.5rem;
  }
  .description {
    color: var(--color-text);
    font-size: 0.9rem;
  }
  .path {
    color: var(--color-text-muted);
    font-size: 0.82rem;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  }
  .counts {
    font-size: 0.82rem;
    color: var(--color-text-muted);
  }
</style>
