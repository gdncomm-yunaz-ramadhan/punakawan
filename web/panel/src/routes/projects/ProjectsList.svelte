<script lang="ts">
  import { onMount } from "svelte";
  import { listProjects, deleteProject, ApiError, type ProjectSummary } from "../../lib/api/client";
  import type { Availability } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import StatusBadge from "../../lib/components/StatusBadge.svelte";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import { onPanelEvent } from "../../lib/events/sse.svelte";
  import Icon from "../../lib/components/Icon.svelte";
  import Button from "../../lib/components/Button.svelte";
  import Dialog from "../../lib/components/overlay/Dialog.svelte";
  import { filterProjects, sortProjects, projectSortOptions, type ProjectSortKey } from "./projectList";

  let projects: ProjectSummary[] = $state([]);
  let error: string | null = $state(null);
  let loading = $state(true);
  // Only the very first load blanks the page for a spinner. Any panel event
  // re-runs load(), and swapping the list out for "Loading…" would unmount the
  // search box mid-keystroke, throwing away focus and caret position.
  let loaded = $state(false);

  let search = $state("");
  let sortKey: ProjectSortKey = $state("name");

  // The list endpoint returns every project at once, so search and sort run
  // over what is already in memory - no request per keystroke.
  const visible = $derived(sortProjects(filterProjects(projects, search), sortKey));

  // Project pending removal, plus the confirmation text typed so far.
  let pendingRemoval: ProjectSummary | null = $state(null);
  let confirmText = $state("");
  let removing = $state(false);
  let removalError: string | null = $state(null);

  // Deregistration is irreversible from the panel's side, so it takes a
  // deliberate act: the exact name (or id) typed out, not just a click.
  const confirmPhrase = $derived.by(() => (pendingRemoval ? pendingRemoval.name || pendingRemoval.id : ""));
  const confirmed = $derived.by(() => {
    if (!pendingRemoval) return false;
    const typed = confirmText.trim();
    return typed === confirmPhrase || typed === pendingRemoval.id;
  });

  async function load() {
    loading = true;
    error = null;
    try {
      projects = (await listProjects()).items;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
      loaded = true;
    }
  }

  onMount(() => {
    load();
    return onPanelEvent(load);
  });

  function open(id: string) {
    navigate(`/projects/${encodeURIComponent(id)}`);
  }

  function startRemoval(p: ProjectSummary) {
    pendingRemoval = p;
    confirmText = "";
    removalError = null;
  }

  function closeRemoval() {
    if (removing) return;
    pendingRemoval = null;
    confirmText = "";
    removalError = null;
  }

  async function confirmRemoval() {
    if (!pendingRemoval || !confirmed) return;
    removing = true;
    removalError = null;
    try {
      await deleteProject(pendingRemoval.id);
      pendingRemoval = null;
      confirmText = "";
      await load();
    } catch (e) {
      // The server refuses to remove the primary workspace with a 409, since
      // it stays resolvable regardless of the registry. Say so plainly rather
      // than showing a bare conflict message.
      removalError =
        e instanceof ApiError && e.code === "primary_project"
          ? "This is the primary workspace for this panel instance, so it cannot be removed here."
          : e instanceof Error
            ? e.message
            : String(e);
    } finally {
      removing = false;
    }
  }
</script>

<PageHeader title="Projects" description="Every workspace your Punakawan instance tracks, with its live snapshot counts." />

{#if loading && !loaded}
  <p>Loading…</p>
{:else if error}
  <ErrorStateCard title="Failed to load projects" message={error} />
{:else if projects.length === 0}
  <EmptyStateCard
    title="No projects yet"
    message="Register a project with the Punakawan CLI to see it here."
  />
{:else}
  <div class="toolbar">
    <div class="field">
      <label for="project-search">Search projects</label>
      <input
        id="project-search"
        type="search"
        placeholder="Name, id, description, or path"
        bind:value={search}
        autocomplete="off"
      />
    </div>
    <div class="field">
      <label for="project-sort">Sort by</label>
      <select id="project-sort" bind:value={sortKey}>
        {#each projectSortOptions as option (option.key)}
          <option value={option.key}>{option.label}</option>
        {/each}
      </select>
    </div>
  </div>

  {#if visible.length === 0}
    <EmptyStateCard
      title="No projects match your search"
      message={`Nothing matches “${search}”. Try a shorter search, or clear it to see all ${projects.length} projects.`}
    />
  {:else}
    <ul class="projects" aria-label="Projects">
      {#each visible as p (p.id)}
        <li>
          <div class="card">
            <!-- aria-label keeps the whole card body from being read out as one
                 enormous control name; the visible detail is still announced by
                 the elements themselves. -->
            <button
              type="button"
              class="open-area"
              aria-label={`Open ${p.name || p.id}`}
              onclick={() => open(p.id)}
            >
              <span class="row">
                <span class="project-title">
                  <span class="project-icon"><Icon name="folder" size={20} /></span>
                  <strong class="name">{p.name || p.id}</strong>
                </span>
                <span class="markers">
                  {#if p.primary}<span class="tag primary" title="Primary project">primary</span>{/if}
                  {#if p.pinned}<span title="Pinned" aria-label="Pinned">📌</span>{/if}
                </span>
              </span>
              {#if p.description}<span class="description">{p.description}</span>{/if}
              <span class="path">{p.path}</span>
              <span class="row wrap">
                <StatusBadge availability={p.availability as Availability} />
              </span>
              <span class="stats" aria-label="Project snapshot">
                <span><strong>{p.repository_count}</strong> repos</span>
                <span><strong>{p.open_task_count}</strong> open</span>
                <span class:danger={p.blocked_task_count > 0}><strong>{p.blocked_task_count}</strong> blocked</span>
                <span><strong>{p.active_session_count}</strong> active</span>
                <span><strong>{p.knowledge_count}</strong> knowledge</span>
              </span>
            </button>
            <div class="card-actions">
              {#if !p.primary}
                <Button
                  variant="danger"
                  size="sm"
                  onclick={() => startRemoval(p)}
                  ariaLabel={`Remove ${p.name || p.id} from Punakawan`}
                >
                  Remove
                </Button>
              {/if}
              <button type="button" class="open-hint" onclick={() => open(p.id)}>
                Open project <span aria-hidden="true">→</span>
              </button>
            </div>
          </div>
        </li>
      {/each}
    </ul>
  {/if}
{/if}

<Dialog open={pendingRemoval !== null} title="Remove project from Punakawan" onclose={closeRemoval}>
  {#if pendingRemoval}
    <div class="remove-confirm">
      <p>
        This removes <strong>{pendingRemoval.name || pendingRemoval.id}</strong> from Punakawan, so this panel stops
        listing and serving it.
      </p>
      <p class="scope">
        It does <strong>not</strong> delete the repository or any of its files. The workspace directory
        (<code>{pendingRemoval.path}</code>), its <code>.punakawan</code> folder, knowledge, tasks, and evidence all stay
        exactly where they are on disk. Registering the same path again brings the project back.
      </p>

      <div class="field">
        <label for="remove-confirm-input">
          Type <strong>{confirmPhrase}</strong> to confirm
        </label>
        <input
          id="remove-confirm-input"
          type="text"
          bind:value={confirmText}
          autocomplete="off"
          aria-describedby="remove-confirm-hint"
        />
        <p id="remove-confirm-hint" class="hint">The project's id (<code>{pendingRemoval.id}</code>) also works.</p>
      </div>

      {#if removalError}
        <p class="error" role="alert" data-testid="removal-error">{removalError}</p>
      {/if}

      <div class="confirm-actions">
        <Button variant="secondary" onclick={closeRemoval} disabled={removing}>Cancel</Button>
        <Button variant="danger" onclick={confirmRemoval} disabled={removing || !confirmed}>
          {removing ? "Removing…" : "Remove project"}
        </Button>
      </div>
    </div>
  {/if}
</Dialog>

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
    border: 1px solid var(--surface-card-border, var(--color-border));
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 1.05rem 1.15rem;
    display: grid;
    gap: 0.55rem;
    background: var(--surface-card-bg, var(--color-surface));
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
  /* The card body is its own button so the Remove action can sit beside it
     without nesting one button inside another. */
  .open-area {
    display: grid;
    gap: 0.55rem;
    min-width: 0;
    padding: 0;
    border: 0;
    background: none;
    text-align: left;
    font: inherit;
    color: inherit;
    cursor: pointer;
  }
  .open-area:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }
  .card-actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: flex-end;
    margin-bottom: 1rem;
  }
  .field {
    display: grid;
    gap: 0.3rem;
    min-width: 0;
  }
  .toolbar .field:first-child {
    flex: 1 1 16rem;
  }
  .field label {
    font-size: 0.78rem;
    font-weight: 650;
    color: var(--color-text-muted);
  }
  .field input,
  .field select {
    font: inherit;
    color: var(--color-text);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    padding: 0.45rem 0.6rem;
    min-height: 38px;
    min-width: 0;
  }
  .field input:focus-visible,
  .field select:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 1px;
  }
  .remove-confirm {
    display: grid;
    gap: 0.85rem;
  }
  .remove-confirm p {
    margin: 0;
    font-size: 0.9rem;
  }
  .remove-confirm .scope {
    color: var(--color-text-muted);
    font-size: 0.84rem;
  }
  .remove-confirm code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.8rem;
    overflow-wrap: anywhere;
  }
  .remove-confirm .hint {
    color: var(--color-text-muted);
    font-size: 0.78rem;
  }
  .remove-confirm .error {
    color: var(--color-danger);
    font-size: 0.85rem;
  }
  .confirm-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
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
    margin-left: auto;
    padding: 0;
    border: 0;
    background: none;
    color: var(--color-accent);
    font-family: inherit;
    font-size: 0.76rem;
    font-weight: 650;
    cursor: pointer;
  }
  .open-hint:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }
</style>
