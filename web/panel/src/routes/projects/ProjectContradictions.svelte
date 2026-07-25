<script lang="ts">
  import { onMount } from "svelte";
  import {
    listContradictions,
    getContradiction,
    resolveContradiction,
    acceptDivergence,
    ApiError,
    type Contradiction,
    type ContradictionSeverity,
  } from "../../lib/api/client";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import Button from "../../lib/components/Button.svelte";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  let items: Contradiction[] = $state([]);
  let loading = $state(true);
  let error: string | null = $state(null);

  // The selected contradiction's full record, loaded lazily on row click.
  let selectedId: string | null = $state(null);
  let detail: Contradiction | null = $state(null);
  let detailLoading = $state(false);
  let detailError: string | null = $state(null);

  // Resolve/accept action state (§22).
  let resolveStatement = $state("");
  let actionBy = $state("");
  let busy = $state(false);
  let actionError: string | null = $state(null);

  async function load() {
    loading = true;
    error = null;
    try {
      const res = await listContradictions(projectId);
      items = res.items ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function select(id: string) {
    if (selectedId === id) {
      selectedId = null;
      detail = null;
      return;
    }
    selectedId = id;
    detail = null;
    detailError = null;
    actionError = null;
    resolveStatement = "";
    detailLoading = true;
    try {
      detail = await getContradiction(projectId, id);
    } catch (e) {
      detailError = e instanceof Error ? e.message : String(e);
    } finally {
      detailLoading = false;
    }
  }

  onMount(load);

  // Severity maps to a semantic pill: critical/material read as danger,
  // minor as warning, informational as neutral.
  function severityVariant(severity: ContradictionSeverity): BadgeVariant {
    switch (severity) {
      case "critical":
      case "material":
        return "danger";
      case "minor":
        return "warning";
      default:
        return "neutral";
    }
  }

  function statusVariant(status: string): BadgeVariant {
    switch (status) {
      case "resolved":
      case "accepted_divergence":
        return "success";
      case "needs_clarification":
      case "resolution_proposed":
      case "triaged":
        return "warning";
      case "detected":
        return "danger";
      case "superseded":
        return "info";
      default:
        return "neutral";
    }
  }

  function humanizeStatus(status: string): string {
    const spaced = status.replace(/_/g, " ");
    return spaced.charAt(0).toUpperCase() + spaced.slice(1);
  }

  async function refreshDetail() {
    if (!selectedId) return;
    detail = await getContradiction(projectId, selectedId);
    // Keep the row's summary in the list in sync with the detail.
    items = items.map((c) => (c.id === detail!.id ? detail! : c));
  }

  async function doResolve() {
    if (!selectedId || !resolveStatement.trim()) return;
    busy = true;
    actionError = null;
    try {
      await resolveContradiction(projectId, selectedId, {
        statement: resolveStatement.trim(),
        by: actionBy.trim() || "panel",
      });
      resolveStatement = "";
      await refreshDetail();
    } catch (e) {
      actionError = e instanceof ApiError ? e.message : e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function doAcceptDivergence() {
    if (!selectedId) return;
    busy = true;
    actionError = null;
    try {
      await acceptDivergence(projectId, selectedId, { by: actionBy.trim() || "panel" });
      await refreshDetail();
    } catch (e) {
      actionError = e instanceof ApiError ? e.message : e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  // A contradiction is closed once resolved or accepted — the actions are
  // hidden for those terminal states.
  function isActionable(status: string): boolean {
    return status !== "resolved" && status !== "accepted_divergence" && status !== "superseded";
  }
</script>

<section aria-label="Project contradictions">
  {#if loading}
    <p>Loading…</p>
  {:else if error}
    <ErrorStateCard title="Failed to load contradictions" message={error} />
  {:else if items.length === 0}
    <EmptyStateCard
      title="No contradictions"
      message="Conflicting claims detected across this project's knowledge will appear here."
    />
  {:else}
    <div class="table-scroll">
      <table>
        <thead>
          <tr>
            <th scope="col">Severity</th>
            <th scope="col">Title</th>
            <th scope="col">Subject</th>
            <th scope="col">Status</th>
            <th scope="col">Sources</th>
            <th scope="col">Updated</th>
          </tr>
        </thead>
        <tbody>
          {#each items as c (c.id)}
            <tr
              class="row"
              class:selected={selectedId === c.id}
              role="button"
              tabindex="0"
              aria-expanded={selectedId === c.id}
              onclick={() => select(c.id)}
              onkeydown={(e) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), select(c.id))}
              data-testid={`contradiction-row-${c.id}`}
            >
              <td>
                <StatusBadge variant={severityVariant(c.severity)} label={c.severity} />
              </td>
              <td class="title">
                {c.title || c.id}
                {#if c.blocking}<span class="blocking" title="Blocking">blocking</span>{/if}
              </td>
              <td class="subject">{c.subject.type}: <code>{c.subject.key}</code></td>
              <td><StatusBadge variant={statusVariant(c.status)} label={humanizeStatus(c.status)} /></td>
              <td class="count">{c.claims?.length ?? 0}</td>
              <td class="updated">{c.updated_at ?? "—"}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    {#if selectedId}
      <div class="detail" data-testid="contradiction-detail">
        {#if detailLoading}
          <p>Loading contradiction…</p>
        {:else if detailError}
          <ErrorStateCard title="Failed to load contradiction" message={detailError} />
        {:else if detail}
          <header class="detail-head">
            <h3>{detail.title || detail.id}</h3>
            <StatusBadge variant={severityVariant(detail.severity)} label={detail.severity} />
            <StatusBadge variant={statusVariant(detail.status)} label={humanizeStatus(detail.status)} />
            {#if detail.blocking}<span class="blocking" title="Blocking">blocking</span>{/if}
          </header>
          <p class="subject-line">
            Subject — {detail.subject.type}: <code>{detail.subject.key}</code>
          </p>

          <h4>Conflicting claims</h4>
          <div class="claims">
            {#each detail.claims as claim, i (i)}
              <article class="claim">
                <p class="claim-source">
                  {claim.source.type}: <code>{claim.source.ref}</code>
                </p>
                <p class="claim-statement">{claim.statement}</p>
                {#if claim.evidence?.length}
                  <ul class="evidence">
                    {#each claim.evidence as ev (ev)}
                      <li><code>{ev}</code></li>
                    {/each}
                  </ul>
                {/if}
              </article>
            {/each}
          </div>

          {#if detail.resolution}
            {@const r = detail.resolution}
            <h4>Proposed resolution</h4>
            <dl class="meta">
              {#if r.proposed_statement}
                <dt>Proposed statement</dt>
                <dd>{r.proposed_statement}</dd>
              {/if}
              {#if r.resolved_statement}
                <dt>Resolved statement</dt>
                <dd>{r.resolved_statement}</dd>
              {/if}
              {#if r.rationale}
                <dt>Rationale</dt>
                <dd>{r.rationale}</dd>
              {/if}
              {#if r.requires_human_confirmation}
                <dt>Confirmation</dt>
                <dd>Requires human confirmation</dd>
              {/if}
            </dl>
          {/if}

          {#if isActionable(detail.status)}
            <div class="actions">
              <label class="field">
                <span>Resolved statement</span>
                <textarea
                  bind:value={resolveStatement}
                  rows="2"
                  aria-label="Resolved statement"
                  placeholder="The single agreed-upon statement…"
                ></textarea>
              </label>
              <label class="field by">
                <span>By</span>
                <input type="text" bind:value={actionBy} aria-label="Resolved by" placeholder="your name" />
              </label>
              {#if actionError}
                <p class="error" role="alert" data-testid="action-error">{actionError}</p>
              {/if}
              <div class="action-buttons">
                <Button variant="secondary" onclick={doAcceptDivergence} disabled={busy}>
                  Accept divergence
                </Button>
                <Button variant="primary" onclick={doResolve} disabled={busy || !resolveStatement.trim()}>
                  {busy ? "Saving…" : "Resolve"}
                </Button>
              </div>
            </div>
          {/if}
        {/if}
      </div>
    {/if}
  {/if}
</section>

<style>
  .table-scroll {
    overflow-x: auto;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.85rem;
  }
  th,
  td {
    text-align: left;
    padding: 0.55rem 0.65rem;
    border-bottom: 1px solid var(--color-border);
    vertical-align: top;
  }
  th {
    color: var(--color-text-muted);
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }
  tr.row {
    cursor: pointer;
  }
  tr.row:hover {
    background: var(--color-surface-subtle);
  }
  tr.row.selected {
    background: var(--color-accent-soft);
  }
  tr.row:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: -2px;
  }
  td.title {
    font-weight: 600;
  }
  td.subject {
    color: var(--color-text-muted);
    word-break: break-word;
  }
  td.count {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  td.updated {
    color: var(--color-text-muted);
    white-space: nowrap;
  }
  .blocking {
    display: inline-block;
    margin-left: 0.4rem;
    font-size: 0.68rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--color-danger);
    background: color-mix(in srgb, var(--color-danger) 14%, transparent);
    border-radius: 999px;
    padding: 0.05rem 0.45rem;
  }
  .detail {
    margin-top: 1rem;
    padding: 1rem 1.1rem;
    background: var(--color-surface-subtle);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
  }
  .detail-head {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    flex-wrap: wrap;
  }
  .detail-head h3 {
    margin: 0;
    font-size: 1.05rem;
  }
  .subject-line {
    margin: 0.5rem 0 0;
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
  h4 {
    margin: 1rem 0 0.5rem;
    font-size: 0.9rem;
  }
  .claims {
    display: grid;
    /* Explicit responsive cap (4/2/1 rule): 1 column on mobile (<640px),
       2 on tablet (>=640px), 4 on desktop (>=1024px). minmax(0,1fr) lets
       tracks shrink so long statements wrap instead of overflowing. */
    grid-template-columns: 1fr;
    gap: 0.75rem;
  }
  @media (min-width: 640px) {
    .claims {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  @media (min-width: 1024px) {
    .claims {
      grid-template-columns: repeat(4, minmax(0, 1fr));
    }
  }
  .claim {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
    padding: 0.7rem 0.8rem;
    display: grid;
    gap: 0.35rem;
    min-width: 0;
  }
  .claim-source {
    margin: 0;
    font-size: 0.72rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--color-text-muted);
    font-weight: 600;
  }
  .claim-statement {
    margin: 0;
    font-size: 0.88rem;
    overflow-wrap: anywhere;
  }
  .evidence {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 0.2rem;
    font-size: 0.78rem;
  }
  dl.meta {
    margin: 0;
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.35rem 0.85rem;
    align-items: baseline;
    font-size: 0.85rem;
  }
  /* On mobile the two-column definition grid crushes labels/values —
     stack to a single column below 640px. */
  @media (max-width: 639px) {
    dl.meta {
      grid-template-columns: 1fr;
      gap: 0.15rem 0;
    }
  }
  dl.meta dt {
    color: var(--color-text-muted);
    font-weight: 600;
  }
  dl.meta dd {
    margin: 0;
    word-break: break-word;
    overflow-wrap: anywhere;
  }
  code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    background: var(--color-surface);
    border-radius: 4px;
    padding: 0.05rem 0.35rem;
    overflow-wrap: anywhere;
  }
  .actions {
    margin-top: 1rem;
    display: grid;
    gap: 0.6rem;
    padding-top: 0.85rem;
    border-top: 1px solid var(--color-border);
  }
  .field {
    display: grid;
    gap: 0.2rem;
    font-size: 0.78rem;
    color: var(--color-text-muted);
  }
  .field.by {
    max-width: 16rem;
  }
  @media (max-width: 639px) {
    .field.by {
      max-width: none;
    }
  }
  textarea,
  input {
    font: inherit;
    padding: 0.4rem 0.55rem;
    border: 1px solid var(--color-border-strong);
    border-radius: var(--radius-sm);
    background: var(--color-surface);
    color: var(--color-text);
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
  }
  input {
    min-height: 40px;
  }
  textarea:focus-visible,
  input:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 1px;
  }
  .error {
    color: var(--color-danger);
    font-size: 0.85rem;
    margin: 0;
  }
  .action-buttons {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  @media (max-width: 639px) {
    .action-buttons {
      flex-direction: column;
    }
    /* Stretch the shared Button (global .btn) full-width when stacked. */
    .action-buttons :global(.btn) {
      width: 100%;
    }
  }
</style>
