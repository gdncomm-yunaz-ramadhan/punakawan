<script lang="ts">
  import { onMount } from "svelte";
  import {
    queryImpact,
    listImpactNodes,
    refreshImpact,
    ApiError,
    type ImpactNode,
    type ImpactResult,
  } from "../../lib/api/client";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  // The include-type facets the query can request (plan §30). Each toggles
  // whether the corresponding list is populated in the result.
  const INCLUDE_TYPES = [
    { id: "repositories", label: "Repositories" },
    { id: "tests", label: "Tests" },
    { id: "deployments", label: "Deployments" },
    { id: "owners", label: "Owners" },
    { id: "missing_coverage", label: "Missing coverage" },
    { id: "contradictions", label: "Contradictions" },
  ];

  // Subject picker options (loaded up front so the input can be a select).
  let nodes: ImpactNode[] = $state([]);
  let nodesError: string | null = $state(null);

  let subjectId = $state("");
  let depth = $state(2);
  let include = $state<Set<string>>(new Set(INCLUDE_TYPES.map((t) => t.id)));

  let result: ImpactResult | null = $state(null);
  let querying = $state(false);
  let queryError: string | null = $state(null);
  let hasQueried = $state(false);

  let refreshing = $state(false);
  let refreshNotice: string | null = $state(null);

  async function loadNodes() {
    try {
      const res = await listImpactNodes(projectId);
      nodes = res.items ?? [];
    } catch (e) {
      nodesError = e instanceof Error ? e.message : String(e);
    }
  }

  onMount(loadNodes);

  function toggleInclude(id: string) {
    const next = new Set(include);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    include = next;
  }

  async function runQuery() {
    if (!subjectId.trim()) return;
    querying = true;
    queryError = null;
    refreshNotice = null;
    try {
      result = await queryImpact(projectId, {
        subject_id: subjectId.trim(),
        depth,
        include: [...include],
      });
      hasQueried = true;
    } catch (e) {
      queryError = e instanceof ApiError ? e.message : e instanceof Error ? e.message : String(e);
    } finally {
      querying = false;
    }
  }

  async function doRefresh() {
    refreshing = true;
    refreshNotice = null;
    queryError = null;
    try {
      await refreshImpact(projectId);
      refreshNotice = "Impact graph refreshed. Re-run your query to see the latest.";
      await loadNodes();
    } catch (e) {
      queryError = e instanceof ApiError ? e.message : e instanceof Error ? e.message : String(e);
    } finally {
      refreshing = false;
    }
  }

  function nodeLabel(n: ImpactNode): string {
    return n.label || n.id;
  }
</script>

<section aria-label="Project impact">
  <form
    class="query-form"
    onsubmit={(e) => {
      e.preventDefault();
      runQuery();
    }}
  >
    <div class="row">
      <label class="field subject">
        <span>Subject</span>
        {#if nodes.length > 0}
          <input
            list="impact-nodes"
            type="text"
            bind:value={subjectId}
            aria-label="Impact subject"
            placeholder="Node id or key"
          />
          <datalist id="impact-nodes">
            {#each nodes as n (n.id)}
              <option value={n.id}>{nodeLabel(n)}</option>
            {/each}
          </datalist>
        {:else}
          <input type="text" bind:value={subjectId} aria-label="Impact subject" placeholder="Node id or key" />
        {/if}
      </label>
      <label class="field depth">
        <span>Depth</span>
        <input type="number" min="1" max="10" bind:value={depth} aria-label="Impact depth" />
      </label>
    </div>

    <fieldset class="include">
      <legend>Include</legend>
      {#each INCLUDE_TYPES as t (t.id)}
        <label class="check">
          <input
            type="checkbox"
            checked={include.has(t.id)}
            onchange={() => toggleInclude(t.id)}
            aria-label={t.label}
          />
          <span>{t.label}</span>
        </label>
      {/each}
    </fieldset>

    <div class="form-actions">
      <button type="button" class="btn" onclick={doRefresh} disabled={refreshing}>
        {refreshing ? "Refreshing…" : "Refresh"}
      </button>
      <button type="submit" class="btn primary" disabled={querying || !subjectId.trim()}>
        {querying ? "Querying…" : "Query"}
      </button>
    </div>
  </form>

  {#if nodesError}
    <p class="hint" role="alert">Could not load subject list: {nodesError}</p>
  {/if}
  {#if refreshNotice}
    <p class="notice" data-testid="refresh-notice">{refreshNotice}</p>
  {/if}

  {#if queryError}
    <ErrorStateCard title="Impact query failed" message={queryError} />
  {:else if querying}
    <p>Querying…</p>
  {:else if result}
    <div class="results" data-testid="impact-result">
      {@render list("Affected repositories", result.affected_repositories.map((r) => ({ id: r, type: "repository" })), "repository")}
      {@render list("Direct impact", result.direct_impact)}
      {@render list("Transitive impact", result.transitive_impact)}
      {@render list("Affected tests", result.affected_tests)}
      {@render list("Deployment artifacts", result.deployment_artifacts)}
      {@render list("Owners", result.owners)}
      {@render list("Missing coverage", result.missing_coverage)}
      {@render list(
        "Related contradictions",
        result.related_contradictions.map((c) => ({ id: c, type: "contradiction" })),
        "contradiction",
      )}
    </div>
  {:else if hasQueried}
    <EmptyStateCard title="No impact found" message="This subject has no recorded impact for the selected options." />
  {:else}
    <EmptyStateCard
      title="Run an impact query"
      message="Pick a subject, choose a depth and the facets to include, then Query to see the blast radius as lists."
    />
  {/if}
</section>

{#snippet list(title: string, entries: ImpactNode[], kind?: string)}
  {#if entries && entries.length > 0}
    <div class="result-group" data-testid={`impact-group-${(kind ?? title).toLowerCase().replace(/\s+/g, "-")}`}>
      <h4>{title} <span class="count">{entries.length}</span></h4>
      <ul>
        {#each entries as n (n.id)}
          <li>
            <span class="node-label">{nodeLabel(n)}</span>
            {#if n.repository}<span class="node-repo">{n.repository}</span>{/if}
            {#if n.type && !kind}<span class="node-type">{n.type}</span>{/if}
          </li>
        {/each}
      </ul>
    </div>
  {/if}
{/snippet}

<style>
  .query-form {
    display: grid;
    gap: 0.85rem;
    margin-bottom: 1.25rem;
    padding: 0.9rem 1rem;
    background: var(--color-surface-subtle);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
  }
  .row {
    display: flex;
    gap: 0.75rem;
    flex-wrap: wrap;
  }
  .field {
    display: grid;
    gap: 0.2rem;
    font-size: 0.78rem;
    color: var(--color-text-muted);
  }
  .field.subject {
    flex: 1 1 16rem;
  }
  .field.depth {
    width: 6rem;
  }
  input {
    font: inherit;
    padding: 0.4rem 0.55rem;
    border: 1px solid var(--color-border-strong);
    border-radius: var(--radius-sm);
    background: var(--color-surface);
    color: var(--color-text);
    min-height: 40px;
    width: 100%;
    box-sizing: border-box;
  }
  input:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 1px;
  }
  .include {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem 1rem;
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
    padding: 0.6rem 0.75rem;
    margin: 0;
  }
  .include legend {
    font-size: 0.72rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--color-text-muted);
    font-weight: 600;
    padding: 0 0.3rem;
  }
  .check {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.85rem;
    color: var(--color-text);
    cursor: pointer;
  }
  .check input {
    accent-color: var(--color-accent);
    width: 16px;
    height: 16px;
    min-height: 0;
  }
  .form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
  }
  .btn {
    font: inherit;
    font-weight: 600;
    font-size: 0.82rem;
    padding: 0.4rem 0.8rem;
    min-height: 40px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--color-border-strong);
    background: var(--color-surface);
    color: var(--color-text);
    cursor: pointer;
  }
  .btn:hover:not(:disabled) {
    border-color: var(--color-accent);
  }
  .btn:disabled {
    opacity: 0.55;
    cursor: default;
  }
  .btn.primary {
    background: var(--color-accent);
    border-color: var(--color-accent);
    color: var(--color-accent-contrast);
  }
  .hint {
    color: var(--color-danger);
    font-size: 0.82rem;
  }
  .notice {
    background: color-mix(in srgb, var(--color-info) 12%, var(--color-surface));
    color: var(--color-info);
    border: 1px solid color-mix(in srgb, var(--color-info) 30%, transparent);
    border-radius: var(--radius-sm);
    padding: 0.5rem 0.75rem;
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  .results {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 1rem;
  }
  .result-group {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 0.85rem 1rem;
  }
  .result-group h4 {
    margin: 0 0 0.5rem;
    font-size: 0.82rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--color-text-muted);
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }
  .result-group .count {
    font-size: 0.72rem;
    font-weight: 700;
    color: var(--color-accent);
    background: var(--color-accent-soft);
    border-radius: 999px;
    padding: 0.05rem 0.45rem;
  }
  .result-group ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 0.35rem;
    font-size: 0.85rem;
  }
  .result-group li {
    display: flex;
    align-items: baseline;
    gap: 0.4rem;
    flex-wrap: wrap;
  }
  .node-label {
    font-weight: 500;
    word-break: break-word;
  }
  .node-repo,
  .node-type {
    font-size: 0.72rem;
    color: var(--color-text-muted);
    background: var(--color-surface-subtle);
    border: 1px solid var(--color-border);
    border-radius: 999px;
    padding: 0.02rem 0.4rem;
  }
</style>
