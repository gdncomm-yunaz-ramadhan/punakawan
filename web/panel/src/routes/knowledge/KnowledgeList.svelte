<script lang="ts">
  import { onMount } from "svelte";
  import { listKnowledge, type KnowledgeRecord, type SearchResult } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import { onPanelEvent } from "../../lib/events/sse.svelte";

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
</script>

<div class="layout">
  <aside class="filters">
    <h2>Filters</h2>
    <label>
      Search
      <input type="search" bind:value={q} onchange={() => load(workspaceId)} placeholder="free text" />
    </label>
    <label>
      Type
      <input type="text" bind:value={type} onchange={() => load(workspaceId)} placeholder="e.g. requirement" />
    </label>
    <label>
      Validity state
      <select bind:value={validityState} onchange={() => load(workspaceId)}>
        <option value="">Any</option>
        {#each Object.entries(validityLabels) as [value, label] (value)}
          <option {value}>{label}</option>
        {/each}
      </select>
    </label>
    <label>
      Repository
      <input type="text" bind:value={repository} onchange={() => load(workspaceId)} />
    </label>
    <label class="checkbox">
      <input type="checkbox" bind:checked={staleOnly} onchange={() => load(workspaceId)} />
      Stale only
    </label>
  </aside>

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
                <span class="validity validity-{row.record.validity.state}">
                  {validityLabels[row.record.validity.state] ?? row.record.validity.state}
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
  .filters h2 {
    font-size: 0.9rem;
    margin: 0;
  }
  .filters label {
    display: grid;
    gap: 0.2rem;
    font-size: 0.8rem;
    color: #444;
  }
  .filters input,
  .filters select {
    font-size: 0.85rem;
    padding: 0.3rem 0.4rem;
    border: 1px solid #ccc;
    border-radius: 6px;
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
    display: grid;
    gap: 0.6rem;
  }
  .row {
    width: 100%;
    text-align: left;
    border: 1px solid #ddd;
    border-radius: 6px;
    padding: 0.6rem 0.8rem;
    background: white;
    cursor: pointer;
    font: inherit;
    display: grid;
    gap: 0.2rem;
  }
  .row:hover {
    border-color: #3949ab;
  }
  .row-head {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .type {
    font-size: 0.7rem;
    color: #666;
    text-transform: uppercase;
  }
  .validity {
    margin-left: auto;
    font-size: 0.75rem;
    padding: 0.05rem 0.4rem;
    border-radius: 4px;
    background: #eee;
  }
  .validity-verified {
    background: #e6f4ea;
    color: #1e7d32;
  }
  .validity-disputed,
  .validity-invalid {
    background: #fdecea;
    color: #c62828;
  }
  .validity-superseded,
  .validity-stale {
    background: #fff4e5;
    color: #9a6700;
  }
  .validity-assumed,
  .validity-inferred,
  .validity-observed {
    background: #e8eaf6;
    color: #3949ab;
  }
  .summary {
    margin: 0;
    font-size: 0.85rem;
    color: #333;
  }
  .meta {
    margin: 0;
    font-size: 0.75rem;
    color: #888;
  }
  .explanation {
    margin: 0;
    font-size: 0.75rem;
    color: #3949ab;
  }
  .error {
    color: #b00020;
  }

  @media (max-width: 720px) {
    .layout {
      grid-template-columns: 1fr;
    }
  }
</style>
