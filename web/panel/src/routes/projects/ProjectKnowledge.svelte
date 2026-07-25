<script lang="ts">
  import { onMount } from "svelte";
  import {
    listProjectKnowledge,
    getProjectKnowledge,
    getProjectKnowledgeRelations,
    getProjectKnowledgeHistory,
    type KnowledgeEvent,
    type KnowledgeRecord,
    type SearchResult,
  } from "../../lib/api/client";
  import { onPanelEvent } from "../../lib/events/sse.svelte";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import Icon from "../../lib/components/Icon.svelte";

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
  let history: KnowledgeEvent[] = $state([]);
  let detailLoading = $state(false);
  let detailError: string | null = $state(null);

  function isSearchResult(item: KnowledgeRecord | SearchResult): item is SearchResult {
    return "Record" in item;
  }

  // The record's substance usually lives in a type-specific structured body
  // rather than free-form summary/content. Pick whichever is present and
  // return it with a human label so the detail view never renders blank for
  // a role/context record. Pretty-printed generically (the panel doesn't
  // hand-craft a view per body type beyond retrieval recipes elsewhere).
  const TYPED_BODIES: { key: keyof KnowledgeRecord; label: string }[] = [
    { key: "requirement", label: "Requirement" },
    { key: "petruk_plan", label: "Petruk plan" },
    { key: "context_dossier", label: "Context dossier" },
    { key: "semar_synthesis", label: "Semar synthesis" },
    { key: "gareng_review", label: "Gareng review" },
    { key: "bagong_review", label: "Bagong review" },
    { key: "convention_profile", label: "Convention profile" },
    { key: "retrieval_recipe", label: "Retrieval recipe" },
  ];
  function typedBody(rec: KnowledgeRecord): { label: string; json: string } | null {
    for (const { key, label } of TYPED_BODIES) {
      const v = rec[key];
      if (v != null) return { label, json: JSON.stringify(v, null, 2) };
    }
    return null;
  }

  const eventLabels: Record<string, string> = {
    put: "Created or updated",
    supersede: "Superseded",
    delete: "Deleted",
  };

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
      history = [];
      return;
    }
    selectedId = recordId;
    detail = null;
    relations = [];
    history = [];
    detailError = null;
    detailLoading = true;
    try {
      const [rec, rel, hist] = await Promise.all([
        getProjectKnowledge(projectId, recordId),
        getProjectKnowledgeRelations(projectId, recordId).catch(() => ({ items: [] as KnowledgeRecord[] })),
        getProjectKnowledgeHistory(projectId, recordId).catch(() => ({ items: [] as KnowledgeEvent[] })),
      ]);
      detail = rec;
      relations = rel.items ?? [];
      history = hist.items ?? [];
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

<section aria-label="Project knowledge" class="knowledge">
  <div class="toolbar" role="search">
    <div class="search-field">
      <span class="search-icon" aria-hidden="true"><Icon name="search" size={18} /></span>
      <input
        class="search-input"
        type="search"
        bind:value={q}
        onchange={() => load(projectId)}
        placeholder="Search knowledge"
        aria-label="Search knowledge"
      />
    </div>
    <div class="filters">
      <label class="field">
        <span>Type</span>
        <input type="text" bind:value={type} onchange={() => load(projectId)} placeholder="e.g. requirement" />
      </label>
      <label class="field">
        <span>Validity state</span>
        <select bind:value={validityState} onchange={() => load(projectId)}>
          <option value="">Any</option>
          {#each Object.entries(validityLabels) as [value, label] (value)}
            <option {value}>{label}</option>
          {/each}
        </select>
      </label>
      <label class="field">
        <span>Repository</span>
        <input type="text" bind:value={repository} onchange={() => load(projectId)} />
      </label>
      <label class="checkbox">
        <input type="checkbox" bind:checked={staleOnly} onchange={() => load(projectId)} />
        Stale only
      </label>
    </div>
  </div>

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

                  {#if typedBody(detail)}
                    {@const body = typedBody(detail)}
                    <h5>{body?.label}</h5>
                    <pre class="content">{body?.json}</pre>
                  {/if}

                  <h5>Provenance</h5>
                  <dl class="provenance">
                    <dt>Type</dt><dd>{detail.type}</dd>
                    <dt>Status</dt><dd>{detail.status}</dd>
                    <dt>Validity</dt>
                    <dd>
                      {validityLabels[detail.validity.state] ?? detail.validity.state}
                      {#if detail.validity.verified_by?.length}· verified by {detail.validity.verified_by.join(", ")}{/if}
                    </dd>
                    <dt>Source</dt><dd>{detail.source.provider}</dd>
                    {#if detail.source.external_id}<dt>External ID</dt><dd>{detail.source.external_id}</dd>{/if}
                    {#if detail.source.uri}<dt>URI</dt><dd class="break">{detail.source.uri}</dd>{/if}
                    {#if detail.source.section}<dt>Section</dt><dd>{detail.source.section}</dd>{/if}
                    <dt>Retrieved</dt><dd>{new Date(detail.source.retrieved_at).toLocaleString()}</dd>
                    <dt>Extraction</dt><dd>{detail.extraction.method}</dd>
                    {#if detail.scope?.repository}<dt>Repository</dt><dd>{detail.scope.repository}</dd>{/if}
                    {#if detail.aliases?.length}<dt>Aliases</dt><dd>{detail.aliases.join(", ")}</dd>{/if}
                  </dl>

                  <h5>Relations</h5>
                  {#if !detail.relations?.length}
                    <p class="muted">No outgoing relations declared.</p>
                  {:else}
                    <ul class="relations">
                      {#each detail.relations as rel, i (i)}
                        <li><span class="muted">{rel.type}</span> <button type="button" class="link-button" onclick={() => open(rel.target)}>{rel.target}</button></li>
                      {/each}
                    </ul>
                  {/if}
                  <h6>Referenced by</h6>
                  {#if relations.length === 0}
                    <p class="muted">No other record declares a relation to this one.</p>
                  {:else}
                    <ul class="relations">
                      {#each relations as rel (rel.id)}
                        <li><span class="muted">{rel.type}</span> <button type="button" class="link-button" onclick={() => open(rel.id)}>{rel.title || rel.id}</button></li>
                      {/each}
                    </ul>
                  {/if}

                  {#if history.length > 0}
                    <h5>History</h5>
                    <ol class="history">
                      {#each history as ev, i (i)}
                        <li><span class="muted">{new Date(ev.timestamp).toLocaleString()}</span> {eventLabels[ev.type] ?? ev.type}{#if ev.superseded_by} · by {ev.superseded_by}{/if}</li>
                      {/each}
                    </ol>
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
  .knowledge {
    display: flex;
    flex-direction: column;
    gap: 1.25rem;
  }
  .toolbar {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  .search-field {
    position: relative;
    display: flex;
    align-items: center;
  }
  .search-icon {
    position: absolute;
    left: 0.7rem;
    display: inline-flex;
    color: var(--color-text-muted);
    pointer-events: none;
  }
  .search-input {
    width: 100%;
    min-width: 0;
    min-height: 40px;
    padding: 0.55rem 0.75rem 0.55rem 2.2rem;
    font: inherit;
    font-size: 0.95rem;
    border: 1px solid var(--color-border-strong);
    border-radius: var(--radius-sm);
    background: var(--color-surface);
    color: var(--color-text);
  }
  .filters {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: 0.75rem;
  }
  .field {
    display: grid;
    gap: 0.25rem;
    font-size: 0.75rem;
    color: var(--color-text-muted);
  }
  .field input,
  .field select {
    min-height: 40px;
    font-size: 0.85rem;
    padding: 0.35rem 0.5rem;
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
    background: var(--color-surface);
    color: var(--color-text);
  }
  .checkbox {
    display: flex;
    align-items: center;
    gap: 0.45rem;
    min-height: 40px;
    font-size: 0.85rem;
    color: var(--color-text);
  }
  .checkbox input {
    width: 1rem;
    height: 1rem;
  }
  .toolbar input:focus-visible,
  .toolbar select:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 1px;
    border-color: var(--color-accent);
  }
  .results {
    min-width: 0;
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
  h6 {
    margin: 0.6rem 0 0.3rem;
    font-size: 0.78rem;
    color: var(--color-text-muted);
  }
  ul.relations {
    gap: 0.25rem;
    font-size: 0.82rem;
  }
  .muted {
    color: var(--color-text-muted);
  }
  dl.provenance {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.2rem 0.75rem;
    font-size: 0.82rem;
    margin: 0;
  }
  dl.provenance dt {
    color: var(--color-text-muted);
  }
  dl.provenance dd {
    margin: 0;
  }
  dl.provenance dd.break {
    word-break: break-all;
  }
  .link-button {
    background: none;
    border: none;
    padding: 0;
    color: var(--color-accent);
    cursor: pointer;
    font: inherit;
    text-decoration: underline;
  }
  ol.history {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 0.25rem;
    font-size: 0.82rem;
  }

  @media (max-width: 639px) {
    .filters {
      flex-direction: column;
      align-items: stretch;
    }
    .field,
    .checkbox {
      width: 100%;
    }
    .field input,
    .field select {
      width: 100%;
    }
  }
</style>
