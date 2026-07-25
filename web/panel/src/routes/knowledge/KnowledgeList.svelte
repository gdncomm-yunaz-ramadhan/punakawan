<script lang="ts">
  import { onMount } from "svelte";
  import { listKnowledge, type KnowledgeRecord, type SearchResult } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import { onPanelEvent } from "../../lib/events/sse.svelte";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import Icon from "../../lib/components/Icon.svelte";

  interface Props {
    workspaceId: string;
  }
  let { workspaceId }: Props = $props();

  let q = $state("");
  let type = $state("");
  let validityState = $state("");
  let repository = $state("");
  let staleOnly = $state(false);

  let rows: { record: KnowledgeRecord; explanation?: string[] }[] = $state([]);
  let error: string | null = $state(null);
  let loading = $state(true);

  function isSearchResult(item: KnowledgeRecord | SearchResult): item is SearchResult {
    return "Record" in item;
  }

  async function load(id: string) {
    loading = true;
    error = null;
    try {
      const res = await listKnowledge(id, {
        q: q || undefined,
        type: type || undefined,
        state: validityState || undefined,
        repository: repository || undefined,
        stale: staleOnly || undefined,
      });
      rows = res.items.map((item) =>
        isSearchResult(item) ? { record: item.Record, explanation: item.Explanation } : { record: item },
      );
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

<div class="knowledge">
  <div class="toolbar" role="search">
    <div class="search-field">
      <span class="search-icon" aria-hidden="true"><Icon name="search" size={18} /></span>
      <input
        class="search-input"
        type="search"
        bind:value={q}
        onchange={() => load(workspaceId)}
        placeholder="Search knowledge"
        aria-label="Search knowledge"
      />
    </div>
    <div class="filters">
      <label class="field">
        <span>Type</span>
        <input type="text" bind:value={type} onchange={() => load(workspaceId)} placeholder="e.g. requirement" />
      </label>
      <label class="field">
        <span>Validity state</span>
        <select bind:value={validityState} onchange={() => load(workspaceId)}>
          <option value="">Any</option>
          {#each Object.entries(validityLabels) as [value, label] (value)}
            <option {value}>{label}</option>
          {/each}
        </select>
      </label>
      <label class="field">
        <span>Repository</span>
        <input type="text" bind:value={repository} onchange={() => load(workspaceId)} />
      </label>
      <label class="checkbox">
        <input type="checkbox" bind:checked={staleOnly} onchange={() => load(workspaceId)} />
        Stale only
      </label>
    </div>
  </div>

  <main class="results">
    {#if loading}
      <p>Loading…</p>
    {:else if error}
      <p role="alert" class="error">Failed to load knowledge: {error}</p>
    {:else if rows.length === 0}
      <p>No knowledge records match these filters.</p>
    {:else}
      <ul>
        {#each rows as row (row.record.id)}
          <li>
            <button
              type="button"
              class="row"
              onclick={() =>
                navigate(`/workspaces/${encodeURIComponent(workspaceId)}/knowledge/${encodeURIComponent(row.record.id)}`)}
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
              {#if row.explanation?.length}
                <p class="explanation">{row.explanation.join(" · ")}</p>
              {/if}
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </main>
</div>

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
    display: grid;
    gap: 0.6rem;
  }
  .row {
    width: 100%;
    text-align: left;
    border: 1px solid var(--color-border);
    border-radius: 6px;
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
    color: var(--color-text);
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
  .error {
    color: var(--color-danger);
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
