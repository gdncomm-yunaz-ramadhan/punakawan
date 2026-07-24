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
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import VersionLineageGraphView from "../../lib/components/graphs/VersionLineageGraphView.svelte";
  import type { GraphNode, GraphEdge } from "../../lib/components/graphs/types";

  interface Props {
    workspaceId: string;
    knowledgeId: string;
  }
  let { workspaceId, knowledgeId }: Props = $props();

  // Retrieval-recipe state badges, per internal/recipe/lifecycle.go and
  // the shared KnowledgeRecordValidityState enum (there is no separate
  // recipe-specific state field - validity.state is reused as-is).
  const recipeStateVariant: Record<string, BadgeVariant> = {
    verified: "success",
    stale: "warning",
    disputed: "danger",
    superseded: "neutral",
    invalid: "danger",
    draft: "info",
    validating: "info",
    assumed: "neutral",
    inferred: "neutral",
    observed: "neutral",
  };
  const recipeStateLabel: Record<string, string> = {
    verified: "Verified",
    stale: "Stale - due for revalidation",
    disputed: "Disputed",
    superseded: "Superseded",
    invalid: "Invalid",
    draft: "Draft",
    validating: "Validating (mid-correction)",
    assumed: "Assumed",
    inferred: "Inferred",
    observed: "Observed",
  };

  function operatorSymbol(op?: string): string {
    const symbols: Record<string, string> = {
      equals: "=",
      not_equals: "≠",
      phrase_contains: "contains phrase",
      contains: "contains",
      in: "in",
      not_in: "not in",
      greater_than: ">",
      less_than: "<",
    };
    return op ? (symbols[op] ?? op) : "?";
  }

  function clauseValueText(value: unknown): string {
    if (value === undefined || value === null) return "";
    if (typeof value === "object") return JSON.stringify(value);
    return String(value);
  }

  function startRecipeReview() {
    navigate(`/artifacts/retrieval_recipe/review/new?id=${encodeURIComponent(knowledgeId)}`);
  }

  // Best-effort one-hop lineage: there is no dedicated recipe
  // lineage-list endpoint (unlike GET .../reviews/{id}/proposals for plan
  // review attempts) - see punokawan-q9r.6.2's own scoping note. This
  // renders only what a single record fetch + its already-fetched
  // relations/superseded_by pointer can show: this record, the newer
  // record it was superseded by (if any, by id only - not fetched), and
  // any already-fetched "related" record that declares a supersedes
  // relation pointing at this one (its immediate predecessor). A full
  // multi-hop lineage graph is a real gap, filed separately.
  function recipeLineage(rec: KnowledgeRecord, relatedRecs: KnowledgeRecord[]): { nodes: GraphNode[]; edges: GraphEdge[] } {
    const nodes: GraphNode[] = [
      { id: rec.id, label: `${rec.retrieval_recipe?.recipe_version ? `v${rec.retrieval_recipe.recipe_version} ` : ""}${rec.id} (viewing)`, type: "version" },
    ];
    const edges: GraphEdge[] = [];

    const predecessors = relatedRecs.filter((r) => (r.relations ?? []).some((rel) => rel.type === "supersedes" && rel.target === rec.id));
    for (const pred of predecessors) {
      nodes.push({ id: pred.id, label: `${pred.retrieval_recipe?.recipe_version ? `v${pred.retrieval_recipe.recipe_version} ` : ""}${pred.id}`, type: "version" });
      edges.push({ id: `edge-${pred.id}-${rec.id}`, source: pred.id, target: rec.id, label: "corrected by" });
    }

    if (rec.superseded_by) {
      nodes.push({ id: rec.superseded_by, label: rec.superseded_by, type: "version" });
      edges.push({ id: `edge-${rec.id}-${rec.superseded_by}`, source: rec.id, target: rec.superseded_by, label: "corrected by" });
    }

    return { nodes, edges };
  }

  let record: KnowledgeRecord | null = $state(null);
  let related: KnowledgeRecord[] = $state([]);
  let history: KnowledgeEvent[] = $state([]);

  // A plain $derived (rather than a template {@const}) since this is
  // consumed by a <section>, not an {#if}/{#each}/{#snippet} block -
  // {@const} is only valid as an immediate child of those.
  const lineage = $derived(record ? recipeLineage(record, related) : { nodes: [], edges: [] });
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
    {#if record.type === "retrieval_recipe"}
      <StatusBadge
        variant={recipeStateVariant[record.validity.state] ?? "neutral"}
        label={recipeStateLabel[record.validity.state] ?? record.validity.state}
      />
    {:else}
      <span class="validity">{record.validity.state}</span>
    {/if}
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

  {#if record.type === "retrieval_recipe" && record.retrieval_recipe}
    {@const recipe = record.retrieval_recipe}
    <section aria-labelledby="recipe-identity-heading" class="recipe-block">
      <div class="recipe-block-head">
        <h2 id="recipe-identity-heading">Recipe identity</h2>
        <button type="button" class="request-correction" onclick={startRecipeReview} data-testid="start-recipe-review">
          Request a correction to this recipe
        </button>
      </div>
      <dl>
        <dt>Capability</dt>
        <dd><code>{recipe.capability}</code></dd>
        <dt>Intent</dt>
        <dd><code>{recipe.intent}</code></dd>
        <dt>Provider</dt>
        <dd>{recipe.provider}</dd>
        <dt>Resource / operation</dt>
        <dd>{recipe.resource} / {recipe.operation}</dd>
        <dt>Read-only</dt>
        <dd>{recipe.read_only ? "Yes" : "No"}</dd>
        {#if recipe.recipe_version}
          <dt>Recipe version</dt>
          <dd>{recipe.recipe_version}</dd>
        {/if}
        {#if recipe.applies_to?.workspace_ids?.length}
          <dt>Scope: workspaces</dt>
          <dd>{recipe.applies_to.workspace_ids.join(", ")}</dd>
        {/if}
        {#if recipe.applies_to?.repository_ids?.length}
          <dt>Scope: repositories</dt>
          <dd>{recipe.applies_to.repository_ids.join(", ")}</dd>
        {:else if !recipe.applies_to?.workspace_ids?.length}
          <dt>Scope</dt>
          <dd class="muted">Globally scoped (no workspace/repository restriction declared)</dd>
        {/if}
      </dl>
    </section>

    <section aria-labelledby="recipe-selector-heading" class="recipe-block">
      <h2 id="recipe-selector-heading">Selector</h2>
      <p class="hint">
        The structured condition this recipe's compiled query is built from - matches
        <code>punakawan knowledge recipe explain</code>'s clause vocabulary.
      </p>
      {#if recipe.selector.all?.length}
        <p class="selector-group-label">All of:</p>
        <ul class="clause-list">
          {#each recipe.selector.all as clause, i (i)}
            <li>
              {#if clause.field}
                <code>{clause.field}</code> {operatorSymbol(clause.operator)} <code>{clauseValueText(clause.value)}</code>
              {:else if clause.all?.length || clause.any?.length}
                <span class="muted">(nested group - {clause.all?.length ? "all" : "any"} of {clause.all?.length ?? clause.any?.length})</span>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
      {#if recipe.selector.any?.length}
        <p class="selector-group-label">Any of:</p>
        <ul class="clause-list">
          {#each recipe.selector.any as clause, i (i)}
            <li>
              {#if clause.field}
                <code>{clause.field}</code> {operatorSymbol(clause.operator)} <code>{clauseValueText(clause.value)}</code>
              {:else if clause.all?.length || clause.any?.length}
                <span class="muted">(nested group - {clause.all?.length ? "all" : "any"} of {clause.all?.length ?? clause.any?.length})</span>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
      {#if !recipe.selector.all?.length && !recipe.selector.any?.length}
        <p class="muted">No selector clauses declared.</p>
      {/if}
    </section>

    <section aria-labelledby="recipe-shape-heading" class="recipe-block">
      <h2 id="recipe-shape-heading">Inputs, ordering, and output shape</h2>
      {#if recipe.inputs?.length}
        <h3>Inputs</h3>
        <ul class="plain-list">
          {#each recipe.inputs as input, i (i)}
            <li>
              <code>{input.name}</code> ({input.type}){input.required ? " - required" : ""}
              {#if input.default}<span class="muted"> default: {input.default}</span>{/if}
            </li>
          {/each}
        </ul>
      {:else}
        <p class="muted">No dynamic inputs - this recipe's selector is fully literal.</p>
      {/if}

      {#if recipe.ordering?.length}
        <h3>Ordering</h3>
        <ul class="plain-list">
          {#each recipe.ordering as order, i (i)}
            <li><code>{order.field}</code> {order.direction}</li>
          {/each}
        </ul>
      {/if}

      <h3>Output</h3>
      <dl>
        <dt>Entity type</dt>
        <dd>{recipe.output.entity_type}</dd>
        <dt>Identity field</dt>
        <dd><code>{recipe.output.identity_field}</code></dd>
        <dt>Fields</dt>
        <dd>{recipe.output.fields.join(", ")}</dd>
      </dl>
    </section>

    <section aria-labelledby="recipe-execution-heading" class="recipe-block">
      <h2 id="recipe-execution-heading">Last execution evidence</h2>
      {#if recipe.last_execution}
        <dl>
          <dt>Status</dt>
          <dd>{recipe.last_execution.status ?? "unknown"}</dd>
          {#if recipe.last_execution.executed_at}
            <dt>Executed</dt>
            <dd>{new Date(recipe.last_execution.executed_at).toLocaleString()}</dd>
          {/if}
          {#if recipe.last_execution.result_count !== undefined}
            <dt>Result count</dt>
            <dd>{recipe.last_execution.result_count}</dd>
          {/if}
          {#if recipe.last_execution.session_id}
            <dt>Session</dt>
            <dd>{recipe.last_execution.session_id}</dd>
          {/if}
          {#if recipe.last_execution.task_id}
            <dt>Task</dt>
            <dd>{recipe.last_execution.task_id}</dd>
          {/if}
          {#if recipe.last_execution.evidence_id}
            <dt>Evidence record</dt>
            <dd class="hash">{recipe.last_execution.evidence_id}</dd>
          {/if}
        </dl>
      {:else}
        <p class="muted">This recipe has never been executed (no last_execution recorded yet).</p>
      {/if}
      <p class="hint">
        Full usage history (every execution, not just the latest) is a known gap - the panel has no
        evidence-by-recipe-id query today, only evidence scoped to a single session
        (see GET /workspaces/&lbrace;workspaceId&rbrace;/sessions/&lbrace;sessionId&rbrace;/evidence). Tracked as a follow-up.
      </p>
    </section>

    {#if recipe.validation}
      <section aria-labelledby="recipe-validation-heading" class="recipe-block">
        <h2 id="recipe-validation-heading">Validation and acceptance</h2>
        <dl>
          <dt>Status</dt>
          <dd>{recipe.validation.status ?? "unknown"}</dd>
          {#if recipe.validation.sample_size !== undefined}
            <dt>Sample size</dt>
            <dd>{recipe.validation.sample_size}</dd>
          {/if}
          {#if recipe.validation.accepted_at}
            <dt>Accepted</dt>
            <dd>{new Date(recipe.validation.accepted_at).toLocaleString()}</dd>
          {/if}
          {#if recipe.validation.accepted_by}
            <dt>Accepted by</dt>
            <dd>{recipe.validation.accepted_by}</dd>
          {/if}
          {#if recipe.validation.accepted_result_count !== undefined}
            <dt>Accepted result count</dt>
            <dd>{recipe.validation.accepted_result_count}</dd>
          {/if}
        </dl>
      </section>
    {/if}

    <section aria-labelledby="recipe-lineage-heading" class="recipe-block">
      <h2 id="recipe-lineage-heading">Version lineage</h2>
      {#if lineage.nodes.length > 1}
        <VersionLineageGraphView nodes={lineage.nodes} edges={lineage.edges} title="Recipe lineage" />
      {:else}
        <p class="muted">No prior or later version is known from this record's own data.</p>
      {/if}
      <p class="hint">
        This is a best-effort, one-hop view derived from this record's own superseded_by pointer and its
        "referenced by" relations - there is no dedicated recipe lineage-list endpoint yet (unlike a plan review's
        proposal history), so a longer correction chain is not fully walkable from the panel today. Tracked as a
        follow-up.
      </p>
    </section>
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
    color: var(--color-text-muted);
    text-transform: uppercase;
  }
  .validity {
    font-size: 0.75rem;
    padding: 0.1rem 0.4rem;
    border-radius: 4px;
    background: var(--color-surface-subtle);
    text-transform: capitalize;
  }
  .id {
    color: var(--color-text-muted);
    font-size: 0.8rem;
    margin: 0.1rem 0 0.6rem;
  }
  .summary {
    font-size: 0.95rem;
  }
  .superseded {
    font-size: 0.85rem;
    color: var(--color-warning);
  }
  .error {
    color: var(--color-danger);
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
    color: var(--color-text-muted);
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
    color: var(--color-text-muted);
    font-size: 0.75rem;
    margin-right: 0.4rem;
  }
  .link-button {
    background: none;
    border: none;
    padding: 0;
    color: var(--color-accent);
    cursor: pointer;
    font-size: inherit;
    text-decoration: underline;
  }
  .muted {
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
  .hint {
    color: var(--color-text-muted);
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
    color: var(--color-text-muted);
    min-width: 11rem;
  }

  /* Recipe-specific blocks (retrieval_recipe records) use the panel's
     semantic theme tokens - unlike the sections above, which predate that
     system and are left untouched here to avoid an unrelated re-theming
     diff on this file's generic knowledge rendering. */
  .recipe-block {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 1rem 1.25rem;
    margin: 1.25rem 0;
  }
  .recipe-block h2 {
    font-size: 1rem;
    margin: 0 0 0.5rem;
    color: var(--color-text);
  }
  .recipe-block h3 {
    font-size: 0.85rem;
    margin: 0.75rem 0 0.3rem;
    color: var(--color-text);
  }
  .recipe-block-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    flex-wrap: wrap;
  }
  .recipe-block-head h2 {
    margin: 0;
  }
  .request-correction {
    border: none;
    border-radius: 6px;
    background: var(--color-accent);
    color: var(--color-accent-contrast);
    padding: 0.45rem 0.85rem;
    font-size: 0.82rem;
    cursor: pointer;
    min-height: 44px;
  }
  .recipe-block dl {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.25rem 0.75rem;
    font-size: 0.85rem;
    color: var(--color-text);
  }
  .recipe-block dt {
    color: var(--color-text-muted);
  }
  .recipe-block dd {
    margin: 0;
  }
  .recipe-block code {
    background: var(--color-surface-subtle);
    border-radius: 4px;
    padding: 0.05rem 0.3rem;
    font-size: 0.85em;
  }
  .selector-group-label {
    font-size: 0.82rem;
    font-weight: 600;
    color: var(--color-text);
    margin: 0.5rem 0 0.2rem;
  }
  .clause-list,
  .plain-list {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 0.3rem;
    font-size: 0.85rem;
    color: var(--color-text);
  }
</style>
