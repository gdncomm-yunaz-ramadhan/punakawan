<script lang="ts">
  import { onMount } from "svelte";
  import {
    listProjectKnowledge,
    getProjectKnowledge,
    getProjectKnowledgeRelations,
    type KnowledgeRecord,
    type SearchResult,
  } from "../../lib/api/client";
  import { onPanelEvent } from "../../lib/events/sse.svelte";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  let q = $state("");
  let type = $state("");
  let validityState = $state("");
  let repository = $state("");
  let staleOnly = $state(false);

  let rows: { record: KnowledgeRecord; explanation?: string[] }[] = $state([]);
  let loading = $state(true);
  let error: string | null = $state(null);

  let selectedId: string | null = $state(null);
  let detail: KnowledgeRecord | null = $state(null);
  let relations: KnowledgeRecord[] = $state([]);
  let detailLoading = $state(false);
  let detailError: string | null = $state(null);

  function isSearchResult(item: KnowledgeRecord | SearchResult): item is SearchResult {
    return "Record" in item;
  }

  async function load(id: string) {
    loading = true;
    error = null;
    try {
      const res = await listProjectKnowledge(id, {
        q: q || undefined,
        type: type || undefined,
        state: validityState || undefined,
        repository: repository || undefined,
        stale: staleOnly || undefined,
      });
      rows = (res.items ?? []).map((item) =>
        isSearchResult(item) ? { record: item.Record, explanation: item.Explanation } : { record: item },
      );
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function open(recordId: string) {
    if (selectedId === recordId) {
      selectedId = null;
      detail = null;
      relations = [];
      return;
    }
    selectedId = recordId;
    detail = null;
    relations = [];
    detailError = null;
    detailLoading = true;
    try {
      const [rec, rel] = await Promise.all([
        getProjectKnowledge(projectId, recordId),
        getProjectKnowledgeRelations(projectId, recordId).catch(() => ({ items: [] as KnowledgeRecord[] })),
      ]);
      detail = rec;
      relations = rel.items ?? [];
    } catch (e) {
      detailError = e instanceof Error ? e.message : String(e);
    } finally {
      detailLoading = false;
    }
  }

  onMount(() => {
    load(projectId);
    return onPanelEvent(() => load(projectId));
  });
  $effect(() => {
    load(projectId);
  });

  const validityLabels: Record<string, string> = {
    verified: "Verified",
    assumed: "Assumed",
    disputed: "Disputed",
    superseded: "Superseded",
    inferred: "Inferred",
    invalid: "Invalid",
    observed: "Observed",
    stale: "Stale",
  };
  const validityVariants: Record<string, BadgeVariant> = {
    verified: "success",
    disputed: "danger",
    invalid: "danger",
    superseded: "warning",
    stale: "warning",
    assumed: "info",
    inferred: "info",
    observed: "info",
  };
</script>

<section aria-label="Project knowledge" class="layout">
  <aside class="filters">
    <h3>Filters</h3>
    <label>
      Search
      <input type="search" bind:value={q} onchange={() => load(projectId)} placeholder="free text" />
    </label>
    <label>
      Type
      <input type="text" bind:value={type} onchange={() => load(projectId)} placeholder="e.g. requirement" />
    </label>
    <label>
      Validity state
      <select bind:value={validityState} onchange={() => load(projectId)}>
        <option value="">Any</option>
        {#each Object.entries(validityLabels) as [value, label] (value)}
          <option {value}>{label}</option>
        {/each}
      </select>
    </label>
    <label>
      Repository
      <input type="text" bind:value={repository} onchange={() => load(projectId)} />
    </label>
    <label class="checkbox">
      <input type="checkbox" bind:checked={staleOnly} onchange={() => load(projectId)} />
      Stale only
    </label>
  </aside>

  <div class="results">
    {#if loading}
      <p>Loading…</p>
    {:else if error}
      <ErrorStateCard title="Failed to load knowledge" message={error} />
    {:else if rows.length === 0}
      <EmptyStateCard title="No knowledge records" message="No knowledge records match these filters." />
    {:else}
      <ul>
        {#each rows as row (row.record.id)}
          <li>
            <button
              type="button"
              class="row"
              class:selected={selectedId === row.record.id}
              aria-expanded={selectedId === row.record.id}
              onclick={() => open(row.record.id)}
            >
              <div class="row-head">
                <span class="type">{row.record.type}</span>
                <strong>{row.record.title}</strong>
                <span class="validity">
                  <StatusBadge
                    variant={validityVariants[row.record.validity.state] ?? "neutral"}
                    label={validityLabels[row.record.validity.state] ?? row.record.validity.state}
                  />
                </span>
              </div>
              {#if row.record.summary}<p class="summary">{row.record.summary}</p>{/if}
              <p class="meta">
                {row.record.id} · {row.record.source.provider}
                {#if row.record.scope?.repository}· {row.record.scope.repository}{/if}
              </p>
              {#if row.explanation?.length}<p class="explanation">{row.explanation.join(" · ")}</p>{/if}
            </button>

            {#if selectedId === row.record.id}
              <div class="detail" data-testid="knowledge-detail">
                {#if detailLoading}
                  <p>Loading record…</p>
                {:else if detailError}
                  <ErrorStateCard title="Failed to load record" message={detailError} />
                {:else if detail}
                  {#if detail.tags?.length}
                    <p class="tags">{#each detail.tags as t (t)}<span class="tag">{t}</span>{/each}</p>
                  {/if}
                  {#if detail.content}
                    <pre class="content">{detail.content}</pre>
                  {:else if detail.summary}
                    <p>{detail.summary}</p>
                  {/if}
                  {#if relations.length > 0}
                    <h5>Related records</h5>
                    <ul class="relations">
                      {#each relations as rel (rel.id)}
                        <li><strong>{rel.title || rel.id}</strong> <span class="muted">{rel.type}</span></li>
                      {/each}
                    </ul>
                  {/if}
                {/if}
              </div>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</section>

<style>
  .layout {
    display: grid;
    grid-template-columns: 200px 1fr;
    gap: 1.25rem;
  }
  .filters {
    display: grid;
    gap: 0.75rem;
    align-content: start;
  }
  .filters h3 {
    font-size: 0.9rem;
    margin: 0;
  }
  .filters label {
    display: grid;
    gap: 0.2rem;
    font-size: 0.8rem;
    color: var(--color-text-muted);
  }
  .filters input,
  .filters select {
    font-size: 0.85rem;
    padding: 0.3rem 0.4rem;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    background: var(--color-surface);
    color: var(--color-text);
  }
  .checkbox {
    flex-direction: row;
    align-items: center;
    display: flex;
    gap: 0.4rem;
  }
  ul {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 0.6rem;
  }
  .row {
    width: 100%;
    text-align: left;
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
    padding: 0.6rem 0.8rem;
    background: var(--color-surface);
    color: var(--color-text);
    cursor: pointer;
    font: inherit;
    display: grid;
    gap: 0.2rem;
  }
  .row:hover {
    border-color: var(--color-accent);
  }
  .row.selected {
    border-color: var(--color-accent);
    background: var(--color-accent-soft);
  }
  .row-head {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .type {
    font-size: 0.7rem;
    color: var(--color-text-muted);
    text-transform: uppercase;
  }
  .validity {
    margin-left: auto;
  }
  .summary {
    margin: 0;
    font-size: 0.85rem;
  }
  .meta {
    margin: 0;
    font-size: 0.75rem;
    color: var(--color-text-muted);
  }
  .explanation {
    margin: 0;
    font-size: 0.75rem;
    color: var(--color-accent);
  }
  .detail {
    margin-top: 0.5rem;
    padding: 0.75rem 0.9rem;
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
    background: var(--color-surface-subtle);
  }
  .tags {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
    margin: 0 0 0.5rem;
  }
  .tag {
    font-size: 0.72rem;
    padding: 0.1rem 0.45rem;
    border-radius: 999px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    color: var(--color-text-muted);
  }
  pre.content {
    margin: 0;
    padding: 0.7rem 0.85rem;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.8rem;
    white-space: pre-wrap;
    word-break: break-word;
    overflow-x: auto;
  }
  h5 {
    margin: 0.75rem 0 0.35rem;
    font-size: 0.82rem;
  }
  ul.relations {
    gap: 0.25rem;
    font-size: 0.82rem;
  }
  .muted {
    color: var(--color-text-muted);
  }

  @media (max-width: 720px) {
    .layout {
      grid-template-columns: 1fr;
    }
  }
</style>
