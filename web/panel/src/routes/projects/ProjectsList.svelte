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
  import Icon from "../../lib/components/Icon.svelte";

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
            <span class="project-title">
              <span class="project-icon"><Icon name="folder" size={20} /></span>
              <strong class="name">{p.name || p.id}</strong>
            </span>
            <span class="markers">
              {#if p.primary}<span class="tag primary" title="Primary project">primary</span>{/if}
              {#if p.pinned}<span title="Pinned" aria-label="Pinned">📌</span>{/if}
            </span>
          </div>
          {#if p.description}<span class="description">{p.description}</span>{/if}
          <span class="path">{p.path}</span>
          <div class="row wrap">
            <StatusBadge availability={p.availability as Availability} />
          </div>
          <span class="stats" aria-label="Project snapshot">
            <span><strong>{p.repository_count}</strong> repos</span>
            <span><strong>{p.open_task_count}</strong> open</span>
            <span class:danger={p.blocked_task_count > 0}><strong>{p.blocked_task_count}</strong> blocked</span>
            <span><strong>{p.active_session_count}</strong> active</span>
            <span><strong>{p.knowledge_count}</strong> knowledge</span>
          </span>
          <span class="open-hint">Open project <span aria-hidden="true">→</span></span>
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
    /* Explicit responsive cap (matches the 4/2/1 rule): 1 column on
       mobile (<640px), 2 on tablet (>=640px), 4 on desktop (>=1024px).
       minmax(0, 1fr) lets tracks shrink so long paths/titles wrap
       instead of forcing horizontal page scroll. */
    grid-template-columns: 1fr;
    gap: 1rem;
  }
  @media (min-width: 640px) {
    ul.projects {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  @media (min-width: 1024px) {
    ul.projects {
      grid-template-columns: repeat(4, minmax(0, 1fr));
    }
  }
  ul.projects li {
    display: flex;
    min-width: 0;
  }
  .card {
    position: relative;
    overflow: hidden;
    width: 100%;
    min-width: 0;
    min-height: 40px;
    text-align: left;
    border: 1px solid var(--surface-card-border, var(--color-border));
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 1.05rem 1.15rem;
    display: grid;
    gap: 0.55rem;
    background: var(--surface-card-bg, var(--color-surface));
    cursor: pointer;
    font: inherit;
    color: var(--color-text);
  }
  .card:hover {
    border-color: var(--color-accent);
    box-shadow: var(--shadow-md);
    transform: translateY(-2px);
  }
  @media (prefers-reduced-motion: no-preference) {
    .card {
      transition: transform 150ms ease, box-shadow 150ms ease, border-color 150ms ease;
    }
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
    font-size: 1.05rem;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .project-title {
    display: inline-flex;
    align-items: center;
    gap: 0.65rem;
    min-width: 0;
  }
  .project-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2.3rem;
    height: 2.3rem;
    border-radius: 10px;
    color: var(--color-accent);
    background: var(--color-accent-soft);
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--color-accent) 14%, transparent);
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
    /* Long paths wrap instead of forcing horizontal page scroll. */
    overflow-wrap: anywhere;
  }
  .stats {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
    padding-top: 0.3rem;
    border-top: 1px solid var(--color-border);
  }
  .stats > span {
    padding: 0.2rem 0.42rem;
    border-radius: 6px;
    background: var(--color-surface-subtle);
    color: var(--color-text-muted);
    font-size: 0.72rem;
  }
  .stats strong {
    color: var(--color-text);
    font-variant-numeric: tabular-nums;
  }
  .stats .danger,
  .stats .danger strong {
    color: var(--color-danger);
  }
  .open-hint {
    justify-self: end;
    color: var(--color-accent);
    font-size: 0.76rem;
    font-weight: 650;
  }
</style>
