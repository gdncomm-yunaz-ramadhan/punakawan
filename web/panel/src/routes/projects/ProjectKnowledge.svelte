<script lang="ts">
  import { untrack } from "svelte";
  import {
    listProjectKnowledge,
    getProjectKnowledge,
    getProjectKnowledgeRelations,
    getProjectKnowledgeHistory,
    type KnowledgeEvent,
    type KnowledgeRecord,
    type SearchResult,
  } from "../../lib/api/client";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import Icon from "../../lib/components/Icon.svelte";
  import VersionLineageGraphView from "../../lib/components/graphs/VersionLineageGraphView.svelte";
  import type { GraphNode, GraphEdge } from "../../lib/components/graphs/types";
  import Dialog from "../../lib/components/overlay/Dialog.svelte";

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
  // retrieval_recipe records get their own dedicated rendering below
  // (recipe identity/selector/shape/execution/validation/lineage), so it is
  // deliberately excluded here to avoid a duplicate raw-JSON dump.
  const TYPED_BODIES: { key: keyof KnowledgeRecord; label: string }[] = [
    { key: "requirement", label: "Requirement" },
    { key: "petruk_plan", label: "Petruk plan" },
    { key: "context_dossier", label: "Context dossier" },
    { key: "semar_synthesis", label: "Semar synthesis" },
    { key: "gareng_review", label: "Gareng review" },
    { key: "bagong_review", label: "Bagong review" },
    { key: "convention_profile", label: "Convention profile" },
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

  // Retrieval-recipe state badges, per internal/recipe/lifecycle.go and the
  // shared KnowledgeRecordValidityState enum (there is no separate
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

  // Best-effort one-hop lineage: there is no dedicated recipe lineage-list
  // endpoint (unlike GET .../reviews/{id}/proposals for plan review
  // attempts) - see punokawan-q9r.6.2's own scoping note. This renders only
  // what a single record fetch + its already-fetched relations/
  // superseded_by pointer can show: this record, the newer record it was
  // superseded by (if any, by id only - not fetched), and any
  // already-fetched "referenced by" record that declares a supersedes
  // relation pointing at this one (its immediate predecessor). A full
  // multi-hop lineage graph is a real gap, filed separately.
  function recipeLineage(rec: KnowledgeRecord, referencedBy: KnowledgeRecord[]): { nodes: GraphNode[]; edges: GraphEdge[] } {
    const nodes: GraphNode[] = [
      { id: rec.id, label: `${rec.retrieval_recipe?.recipe_version ? `v${rec.retrieval_recipe.recipe_version} ` : ""}${rec.id} (viewing)`, type: "version" },
    ];
    const edges: GraphEdge[] = [];

    const predecessors = referencedBy.filter((r) => (r.relations ?? []).some((rel) => rel.type === "supersedes" && rel.target === rec.id));
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

  // Only ever rendered inside the retrieval_recipe-specific section below,
  // so a plain truthy check on detail is enough - no need to also gate on
  // detail.type here.
  const lineage = $derived(detail ? recipeLineage(detail, relations) : { nodes: [] as GraphNode[], edges: [] as GraphEdge[] });

  async function fetchRows(id: string) {
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
  }

  async function load(id: string) {
    loading = true;
    error = null;
    try {
      await fetchRows(id);
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

  // Single trigger for the first load and for a project change. The discrete filter controls are read
  // here so they re-run the load; the free-text fields deliberately are not,
  // because they update on every keystroke and would then fire one request
  // per character - they refetch from their own change handlers instead.
  // load()'s own reads are untracked so they cannot re-add those
  // dependencies implicitly.
  $effect(() => {
    const id = projectId;
    validityState;
    staleOnly;
    untrack(() => load(id));
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
        <select bind:value={validityState}>
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
        <input type="checkbox" bind:checked={staleOnly} />
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
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</section>

<Dialog open={selectedId !== null} title={detail?.title ?? "Knowledge record"} size="lg" onclose={() => (selectedId = null)}>
  <div data-testid="knowledge-detail">
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

      {#if detail.superseded_by}
        <p class="superseded">
          Superseded by
          <button type="button" class="link-button" onclick={() => open(detail?.superseded_by ?? "")}>
            {detail.superseded_by}
          </button>
        </p>
      {/if}

      {#if detail.type === "retrieval_recipe" && detail.retrieval_recipe}
        {@const recipe = detail.retrieval_recipe}
        <section aria-labelledby="recipe-identity-heading-{detail.id}" class="recipe-block">
          <h5 id="recipe-identity-heading-{detail.id}">Recipe identity</h5>
          <dl class="provenance">
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

        <section aria-labelledby="recipe-selector-heading-{detail.id}" class="recipe-block">
          <h5 id="recipe-selector-heading-{detail.id}">Selector</h5>
          <p class="hint">
            The structured condition this recipe's compiled query is built from - matches
            the recipe selector clause vocabulary.
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

        <section aria-labelledby="recipe-shape-heading-{detail.id}" class="recipe-block">
          <h5 id="recipe-shape-heading-{detail.id}">Inputs, ordering, and output shape</h5>
          {#if recipe.inputs?.length}
            <h6>Inputs</h6>
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
            <h6>Ordering</h6>
            <ul class="plain-list">
              {#each recipe.ordering as order, i (i)}
                <li><code>{order.field}</code> {order.direction}</li>
              {/each}
            </ul>
          {/if}

          <h6>Output</h6>
          <dl class="provenance">
            <dt>Entity type</dt>
            <dd>{recipe.output.entity_type}</dd>
            <dt>Identity field</dt>
            <dd><code>{recipe.output.identity_field}</code></dd>
            <dt>Fields</dt>
            <dd>{recipe.output.fields.join(", ")}</dd>
          </dl>
        </section>

        <section aria-labelledby="recipe-execution-heading-{detail.id}" class="recipe-block">
          <h5 id="recipe-execution-heading-{detail.id}">Last execution evidence</h5>
          {#if recipe.last_execution}
            <dl class="provenance">
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
                <dd class="break">{recipe.last_execution.evidence_id}</dd>
              {/if}
            </dl>
          {:else}
            <p class="muted">This recipe has never been executed (no last_execution recorded yet).</p>
          {/if}
          <p class="hint">
            Full usage history (every execution, not just the latest) is a known gap - the panel has no
            evidence-by-recipe-id query today, only evidence scoped to a single session
            (see GET /projects/&lbrace;projectId&rbrace;/sessions/&lbrace;sessionId&rbrace;/evidence). Tracked as a follow-up.
          </p>
        </section>

        {#if recipe.validation}
          <section aria-labelledby="recipe-validation-heading-{detail.id}" class="recipe-block">
            <h5 id="recipe-validation-heading-{detail.id}">Validation and acceptance</h5>
            <dl class="provenance">
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

        <section aria-labelledby="recipe-lineage-heading-{detail.id}" class="recipe-block">
          <h5 id="recipe-lineage-heading-{detail.id}">Version lineage</h5>
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

      <h5>Provenance</h5>
      <dl class="provenance">
        <dt>Type</dt><dd>{detail.type}</dd>
        <dt>Status</dt><dd>{detail.status}</dd>
        <dt>Validity</dt>
        <dd>
          {#if detail.type === "retrieval_recipe"}
            <StatusBadge
              variant={recipeStateVariant[detail.validity.state] ?? "neutral"}
              label={recipeStateLabel[detail.validity.state] ?? detail.validity.state}
            />
          {:else}
            {validityLabels[detail.validity.state] ?? detail.validity.state}
          {/if}
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
</Dialog>

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
  .superseded {
    font-size: 0.82rem;
    color: var(--color-warning);
  }
  .hint {
    color: var(--color-text-muted);
    font-size: 0.72rem;
    margin-top: 0;
  }
  .recipe-block {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
    padding: 0.75rem 0.9rem;
    margin: 0.75rem 0;
  }
  .recipe-block code {
    background: var(--color-surface-subtle);
    border-radius: 4px;
    padding: 0.05rem 0.3rem;
    font-size: 0.85em;
  }
  .selector-group-label {
    font-size: 0.8rem;
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
    font-size: 0.82rem;
    color: var(--color-text);
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
