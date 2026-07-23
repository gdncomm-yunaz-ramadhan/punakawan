<script lang="ts">
  import { onMount } from "svelte";
  import {
    getKnowledge,
    getKnowledgeHistory,
    getKnowledgeRelations,
    type KnowledgeEvent,
    type KnowledgeRecord,
  } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";

  interface Props {
    workspaceId: string;
    knowledgeId: string;
  }
  let { workspaceId, knowledgeId }: Props = $props();

  let record: KnowledgeRecord | null = $state(null);
  let related: KnowledgeRecord[] = $state([]);
  let history: KnowledgeEvent[] = $state([]);
  let error: string | null = $state(null);
  let loading = $state(true);

  async function load(id: string) {
    loading = true;
    error = null;
    try {
      const [rec, relations, hist] = await Promise.all([
        getKnowledge(workspaceId, id),
        getKnowledgeRelations(workspaceId, id),
        getKnowledgeHistory(workspaceId, id),
      ]);
      record = rec;
      related = relations.items;
      history = hist.items;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(() => load(knowledgeId));
  $effect(() => {
    load(knowledgeId);
  });

  const eventLabels: Record<string, string> = {
    put: "Created or updated",
    supersede: "Superseded",
    delete: "Deleted",
  };
</script>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <p role="alert" class="error">Failed to load this record: {error}</p>
{:else if record}
  <header>
    <span class="type">{record.type}</span>
    <h1>{record.title}</h1>
    <span class="validity">{record.validity.state}</span>
  </header>
  <p class="id">{record.id}</p>
  {#if record.summary}<p class="summary">{record.summary}</p>{/if}
  {#if record.superseded_by}
    <p class="superseded">
      Superseded by
      <button
        type="button"
        class="link-button"
        onclick={() =>
          navigate(`/workspaces/${encodeURIComponent(workspaceId)}/knowledge/${encodeURIComponent(record?.superseded_by ?? "")}`)}
      >
        {record.superseded_by}
      </button>
    </p>
  {/if}

  <section aria-labelledby="provenance-heading">
    <h2 id="provenance-heading">Provenance</h2>
    <dl>
      <dt>Source provider</dt>
      <dd>{record.source.provider}</dd>
      {#if record.source.external_id}
        <dt>External ID</dt>
        <dd>{record.source.external_id}</dd>
      {/if}
      {#if record.source.uri}
        <dt>URI</dt>
        <dd>{record.source.uri}</dd>
      {/if}
      {#if record.source.version !== undefined && record.source.version !== null}
        <dt>Version</dt>
        <dd>{record.source.version}</dd>
      {/if}
      {#if record.source.section}
        <dt>Section</dt>
        <dd>{record.source.section}</dd>
      {/if}
      {#if record.source.content_hash}
        <dt>Content hash</dt>
        <dd class="hash">{record.source.content_hash}</dd>
      {/if}
      <dt>Retrieved</dt>
      <dd>{new Date(record.source.retrieved_at).toLocaleString()}</dd>
      <dt>Extraction method</dt>
      <dd>{record.extraction.method}</dd>
      {#if record.validity.verified_by?.length}
        <dt>Verified by</dt>
        <dd>{record.validity.verified_by.join(", ")}</dd>
      {/if}
    </dl>
  </section>

  <section aria-labelledby="relations-heading">
    <h2 id="relations-heading">Relations</h2>
    {#if !record.relations || record.relations.length === 0}
      <p class="muted">No outgoing relations declared.</p>
    {:else}
      <ul class="relations">
        {#each record.relations as rel, i (i)}
          <li>
            <span class="rel-type">{rel.type}</span>
            <button
              type="button"
              class="link-button"
              onclick={() => navigate(`/workspaces/${encodeURIComponent(workspaceId)}/knowledge/${encodeURIComponent(rel.target)}`)}
            >
              {rel.target}
            </button>
          </li>
        {/each}
      </ul>
    {/if}

    <h3>Referenced by</h3>
    {#if related.length === 0}
      <p class="muted">No other record declares a relation to this one.</p>
    {:else}
      <ul class="relations">
        {#each related as r (r.id)}
          <li>
            <span class="rel-type">{r.type}</span>
            <button
              type="button"
              class="link-button"
              onclick={() => navigate(`/workspaces/${encodeURIComponent(workspaceId)}/knowledge/${encodeURIComponent(r.id)}`)}
            >
              {r.title}
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <section aria-labelledby="history-heading">
    <h2 id="history-heading">History</h2>
    <p class="hint">
      Derived from bd's own put/supersede/delete event log - a "put" covers both creation and later updates, so it
      cannot distinguish an edit from a re-verification.
    </p>
    {#if history.length === 0}
      <p class="muted">No history recorded.</p>
    {:else}
      <ol class="history">
        {#each history as ev, i (i)}
          <li>
            <span class="time">{new Date(ev.timestamp).toLocaleString()}</span>
            <span class="event">{eventLabels[ev.type] ?? ev.type}</span>
            {#if ev.superseded_by}<span class="muted">by {ev.superseded_by}</span>{/if}
          </li>
        {/each}
      </ol>
    {/if}
  </section>
{/if}

<style>
  header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  h1 {
    font-size: 1.2rem;
    margin: 0;
  }
  .type {
    font-size: 0.75rem;
    color: #666;
    text-transform: uppercase;
  }
  .validity {
    font-size: 0.75rem;
    padding: 0.1rem 0.4rem;
    border-radius: 4px;
    background: #eee;
    text-transform: capitalize;
  }
  .id {
    color: #888;
    font-size: 0.8rem;
    margin: 0.1rem 0 0.6rem;
  }
  .summary {
    font-size: 0.95rem;
  }
  .superseded {
    font-size: 0.85rem;
    color: #9a6700;
  }
  .error {
    color: #b00020;
  }
  section {
    margin: 1.25rem 0;
  }
  h2 {
    font-size: 1rem;
    margin-bottom: 0.3rem;
  }
  h3 {
    font-size: 0.85rem;
    margin: 0.75rem 0 0.3rem;
  }
  dl {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.2rem 0.75rem;
    font-size: 0.85rem;
  }
  dt {
    color: #666;
  }
  dd {
    margin: 0;
  }
  .hash {
    font-family: monospace;
    font-size: 0.75rem;
    word-break: break-all;
  }
  ul.relations {
    list-style: none;
    padding: 0;
    display: grid;
    gap: 0.3rem;
    font-size: 0.85rem;
  }
  .rel-type {
    color: #666;
    font-size: 0.75rem;
    margin-right: 0.4rem;
  }
  .link-button {
    background: none;
    border: none;
    padding: 0;
    color: #3949ab;
    cursor: pointer;
    font-size: inherit;
    text-decoration: underline;
  }
  .muted {
    color: #999;
    font-size: 0.85rem;
  }
  .hint {
    color: #888;
    font-size: 0.75rem;
    margin-top: 0;
  }
  ol.history {
    list-style: none;
    padding: 0;
    display: grid;
    gap: 0.3rem;
    font-size: 0.85rem;
  }
  ol.history li {
    display: flex;
    gap: 0.6rem;
  }
  .time {
    color: #666;
    min-width: 11rem;
  }
</style>
