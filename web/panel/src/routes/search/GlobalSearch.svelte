<script lang="ts">
  import { globalSearch, type GlobalSearchResult } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";

  let q = $state("");
  let results: GlobalSearchResult[] = $state([]);
  let error: string | null = $state(null);
  let loading = $state(false);
  let searched = $state(false);

  async function runSearch(e?: Event) {
    e?.preventDefault();
    if (!q.trim()) return;
    loading = true;
    error = null;
    searched = true;
    try {
      const res = await globalSearch(q);
      results = res.items;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }
</script>

<h1>Search</h1>
<p class="hint">Searches every registered workspace's knowledge base at once, ranked by Reciprocal Rank Fusion.</p>

<form onsubmit={runSearch}>
  <input type="search" bind:value={q} placeholder="Search knowledge across all workspaces" />
  <button type="submit">Search</button>
</form>

{#if loading}
  <p>Searching…</p>
{:else if error}
  <p role="alert" class="error">Search failed: {error}</p>
{:else if searched && results.length === 0}
  <p>No matches.</p>
{:else if results.length > 0}
  <ul>
    {#each results as r, i (i)}
      <li>
        <button
          type="button"
          class="row"
          onclick={() =>
            navigate(
              `/workspaces/${encodeURIComponent(r.workspace_id)}/knowledge/${encodeURIComponent(r.result.Id)}`,
            )}
        >
          <div class="row-head">
            <span class="workspace">{r.workspace_id}</span>
            <strong>{r.result.Title}</strong>
            <span class="kind">{r.result.Match.Kind}</span>
          </div>
          {#if r.result.Summary}<p class="summary">{r.result.Summary}</p>{/if}
          {#if r.result.Explanation?.length}
            <p class="explanation">{r.result.Explanation.join(" · ")}</p>
          {/if}
        </button>
      </li>
    {/each}
  </ul>
{/if}

<style>
  h1 {
    font-size: 1.2rem;
    margin-bottom: 0.2rem;
  }
  .hint {
    color: var(--color-text-muted);
    font-size: 0.85rem;
    margin-top: 0;
  }
  form {
    display: flex;
    gap: 0.5rem;
    margin: 1rem 0 1.5rem;
  }
  input[type="search"] {
    flex: 1;
    font-size: 0.95rem;
    padding: 0.5rem 0.7rem;
    border: 1px solid var(--color-border);
    border-radius: 6px;
  }
  button[type="submit"] {
    padding: 0.5rem 1rem;
    border: 1px solid var(--color-accent);
    background: var(--color-accent);
    color: var(--color-accent-contrast);
    border-radius: 6px;
    cursor: pointer;
    font-size: 0.9rem;
  }
  .error {
    color: var(--color-danger);
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
  .workspace {
    font-size: 0.7rem;
    color: var(--color-accent);
    background: var(--color-accent-soft);
    padding: 0.05rem 0.4rem;
    border-radius: 4px;
  }
  .kind {
    margin-left: auto;
    font-size: 0.7rem;
    color: var(--color-text-muted);
  }
  .summary {
    margin: 0;
    font-size: 0.85rem;
    color: var(--color-text);
  }
  .explanation {
    margin: 0;
    font-size: 0.75rem;
    color: var(--color-accent);
  }
</style>
