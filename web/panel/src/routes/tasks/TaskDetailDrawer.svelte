<script lang="ts">
  import { onMount } from "svelte";
  import { getTask, type TaskDetail } from "../../lib/api/client";

  interface Props {
    workspaceId: string;
    taskId: string;
    onclose: () => void;
  }
  let { workspaceId, taskId, onclose }: Props = $props();

  let detail: TaskDetail | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(true);

  async function load(id: string) {
    loading = true;
    error = null;
    detail = null;
    try {
      detail = await getTask(workspaceId, id);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(() => load(taskId));
  $effect(() => {
    load(taskId);
  });

  function criteria(text: string | undefined): string[] {
    if (!text) return [];
    return text
      .split(/\\n|\n/)
      .map((line) => line.trim())
      .filter(Boolean);
  }
</script>

<div class="backdrop" role="presentation" onclick={onclose}></div>
<aside class="drawer" aria-label="Task detail">
  <div class="drawer-head">
    <h2>{taskId}</h2>
    <button type="button" class="close" onclick={onclose} aria-label="Close">✕</button>
  </div>

  {#if loading}
    <p>Loading…</p>
  {:else if error}
    <p role="alert" class="error">Failed to load this task: {error}</p>
  {:else if detail}
    <p class="title">{detail.title}</p>
    <p class="meta">
      {detail.issue_type} · P{detail.priority} · <span class="status">{detail.status}</span>
      {#if detail.external_ref}· {detail.external_ref}{/if}
    </p>

    {#if detail.description}
      <section>
        <h3>Description</h3>
        <p>{detail.description}</p>
      </section>
    {/if}

    {#if criteria(detail.acceptance_criteria).length > 0}
      <section>
        <h3>Acceptance Criteria</h3>
        <ul>
          {#each criteria(detail.acceptance_criteria) as c, i (i)}
            <li>{c}</li>
          {/each}
        </ul>
      </section>
    {/if}

    <section>
      <h3>Dependencies</h3>
      {#if !detail.dependencies || detail.dependencies.length === 0}
        <p class="muted">No dependencies.</p>
      {:else}
        <ul class="related">
          {#each detail.dependencies as d (d.id)}
            <li><strong>{d.id}</strong> {d.title} <span class="status">{d.status}</span></li>
          {/each}
        </ul>
      {/if}
    </section>

    <section>
      <h3>Dependents</h3>
      {#if !detail.dependents || detail.dependents.length === 0}
        <p class="muted">Nothing depends on this task.</p>
      {:else}
        <ul class="related">
          {#each detail.dependents as d (d.id)}
            <li><strong>{d.id}</strong> {d.title} <span class="status">{d.status}</span></li>
          {/each}
        </ul>
      {/if}
    </section>

    <p class="gap-note">
      Status history, related requirements/knowledge/evidence, and commit or PR links are not yet wired into the task
      reader - not shown to avoid fabricating them.
    </p>
  {/if}
</aside>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.2);
    z-index: 10;
  }
  .drawer {
    position: fixed;
    top: 0;
    right: 0;
    height: 100vh;
    width: min(420px, 100vw);
    background: var(--color-surface);
    border-left: 1px solid var(--color-border);
    padding: 1rem 1.25rem;
    overflow-y: auto;
    z-index: 11;
  }
  .drawer-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .drawer-head h2 {
    font-size: 1rem;
    margin: 0;
  }
  .close {
    background: none;
    border: none;
    font-size: 1rem;
    cursor: pointer;
  }
  .title {
    font-size: 1.05rem;
    font-weight: 600;
  }
  .meta {
    color: var(--color-text-muted);
    font-size: 0.85rem;
    text-transform: capitalize;
  }
  .status {
    color: var(--color-accent);
  }
  section {
    margin: 0.9rem 0;
  }
  section h3 {
    font-size: 0.85rem;
    margin: 0 0 0.3rem;
    color: var(--color-text-muted);
  }
  ul.related {
    list-style: none;
    padding: 0;
    display: grid;
    gap: 0.3rem;
    font-size: 0.85rem;
  }
  .muted {
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
  .error {
    color: var(--color-danger);
  }
  .gap-note {
    color: var(--color-text-muted);
    font-size: 0.75rem;
    border-top: 1px solid var(--color-border);
    padding-top: 0.6rem;
  }
</style>
