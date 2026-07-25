<script lang="ts">
  import { onMount } from "svelte";
  import {
    listDossiers,
    getDossier,
    finalizeDossier,
    exportDossierMarkdown,
    ApiError,
    type ChangeDossier,
  } from "../../lib/api/client";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import Button from "../../lib/components/Button.svelte";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  let items: ChangeDossier[] = $state([]);
  let loading = $state(true);
  let error: string | null = $state(null);

  let selectedId: string | null = $state(null);
  let detail: ChangeDossier | null = $state(null);
  let detailLoading = $state(false);
  let detailError: string | null = $state(null);

  let busy = $state(false);
  let actionError: string | null = $state(null);
  let exporting = $state(false);

  async function load() {
    loading = true;
    error = null;
    try {
      const res = await listDossiers(projectId);
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
    detailLoading = true;
    try {
      detail = await getDossier(projectId, id);
    } catch (e) {
      detailError = e instanceof Error ? e.message : String(e);
    } finally {
      detailLoading = false;
    }
  }

  onMount(load);

  function statusVariant(status: string): BadgeVariant {
    switch (status.toLowerCase()) {
      case "finalized":
      case "complete":
      case "approved":
        return "success";
      case "draft":
      case "in_progress":
      case "assembling":
        return "warning";
      case "blocked":
      case "rejected":
        return "danger";
      case "superseded":
      case "archived":
        return "info";
      default:
        return "neutral";
    }
  }

  // Blocking is either an explicit flag or implied by unresolved
  // contradictions / uncovered requirements (plan §38).
  function isBlocking(d: ChangeDossier): boolean {
    if (d.blocking) return true;
    if ((d.contradictions?.unresolved ?? 0) > 0) return true;
    if ((d.requirements?.uncovered ?? 0) > 0) return true;
    return false;
  }

  function requirementsLabel(d: ChangeDossier): string {
    const r = d.requirements;
    if (!r) return "—";
    const total = r.covered + r.uncovered;
    return `${r.covered}/${total}`;
  }

  function conformanceLabel(d: ChangeDossier): string {
    const p = d.plan_conformance;
    if (!p) return "—";
    return `${p.implemented} impl · ${p.partial} partial · ${p.missing} missing`;
  }

  async function refreshDetail() {
    if (!selectedId) return;
    detail = await getDossier(projectId, selectedId);
    items = items.map((d) => (d.id === detail!.id ? detail! : d));
  }

  async function doFinalize() {
    if (!selectedId) return;
    busy = true;
    actionError = null;
    try {
      await finalizeDossier(projectId, selectedId);
      await refreshDetail();
    } catch (e) {
      actionError = e instanceof ApiError ? e.message : e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function doExport() {
    if (!selectedId) return;
    exporting = true;
    actionError = null;
    try {
      const md = await exportDossierMarkdown(projectId, selectedId);
      // Trigger a client-side download of the Markdown rendering.
      const blob = new Blob([md], { type: "text/markdown" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${selectedId}.md`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (e) {
      actionError = e instanceof ApiError ? e.message : e instanceof Error ? e.message : String(e);
    } finally {
      exporting = false;
    }
  }
</script>

<section aria-label="Project change dossiers">
  {#if loading}
    <p>Loading…</p>
  {:else if error}
    <ErrorStateCard title="Failed to load change dossiers" message={error} />
  {:else if items.length === 0}
    <EmptyStateCard
      title="No change dossiers"
      message="Assembled change dossiers for this project will appear here."
    />
  {:else}
    <div class="table-scroll">
      <table>
        <thead>
          <tr>
            <th scope="col">Title</th>
            <th scope="col">Status</th>
            <th scope="col">Requirements</th>
            <th scope="col">Plan conformance</th>
            <th scope="col">Claims</th>
            <th scope="col">Blocking</th>
          </tr>
        </thead>
        <tbody>
          {#each items as d (d.id)}
            <tr
              class="row"
              class:selected={selectedId === d.id}
              role="button"
              tabindex="0"
              aria-expanded={selectedId === d.id}
              onclick={() => select(d.id)}
              onkeydown={(e) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), select(d.id))}
              data-testid={`dossier-row-${d.id}`}
            >
              <td class="title">{d.title || d.id}</td>
              <td><StatusBadge variant={statusVariant(d.status)} label={d.status} /></td>
              <td class="req">{requirementsLabel(d)}</td>
              <td class="conformance">{conformanceLabel(d)}</td>
              <td class="claims">{d.claims?.length ?? 0}</td>
              <td>
                {#if isBlocking(d)}
                  <StatusBadge variant="danger" label="Blocking" />
                {:else}
                  <StatusBadge variant="success" label="Clear" />
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    {#if selectedId}
      <div class="detail" data-testid="dossier-detail">
        {#if detailLoading}
          <p>Loading dossier…</p>
        {:else if detailError}
          <ErrorStateCard title="Failed to load dossier" message={detailError} />
        {:else if detail}
          <header class="detail-head">
            <h3>{detail.title || detail.id}</h3>
            <StatusBadge variant={statusVariant(detail.status)} label={detail.status} />
            {#if isBlocking(detail)}<StatusBadge variant="danger" label="Blocking" />{/if}
          </header>
          {#if detail.objective?.statement}
            <p class="objective">{detail.objective.statement}</p>
          {/if}

          <h4>Summary</h4>
          <dl class="meta">
            <dt>Requirements covered</dt>
            <dd>{requirementsLabel(detail)}</dd>
            {#if detail.contradictions}
              <dt>Contradictions</dt>
              <dd>{detail.contradictions.resolved} resolved · {detail.contradictions.unresolved} unresolved</dd>
            {/if}
            <dt>Plan conformance</dt>
            <dd>{conformanceLabel(detail)}</dd>
            {#if detail.impact?.repositories?.length}
              <dt>Affected repositories</dt>
              <dd>{detail.impact.repositories.join(", ")}</dd>
            {/if}
            {#if detail.impact?.excluded_repositories?.length}
              <dt>Excluded repositories</dt>
              <dd>{detail.impact.excluded_repositories.join(", ")}</dd>
            {/if}
            {#if detail.impact?.missing_coverage?.length}
              <dt>Missing coverage</dt>
              <dd>{detail.impact.missing_coverage.join(", ")}</dd>
            {/if}
          </dl>

          {#if detail.claims?.length}
            <h4>Verified claims <span class="count">{detail.claims.length}</span></h4>
            <ul class="claims-list">
              {#each detail.claims as claim (claim)}
                <li>{claim}</li>
              {/each}
            </ul>
          {/if}

          {#if detail.evidence?.length}
            <h4>Evidence</h4>
            <ul class="claims-list">
              {#each detail.evidence as ev (ev)}
                <li><code>{ev}</code></li>
              {/each}
            </ul>
          {/if}

          {#if actionError}
            <p class="error" role="alert" data-testid="action-error">{actionError}</p>
          {/if}

          <div class="action-buttons">
            <!-- data-testid lives on a display:contents wrapper span because the
                 shared Button doesn't forward data-testid; these testids are only
                 asserted present (never clicked via testid), so wrapping is safe. -->
            <span class="btn-wrap" data-testid="export-dossier">
              <Button variant="secondary" onclick={doExport} disabled={exporting}>
                {exporting ? "Exporting…" : "Export .md"}
              </Button>
            </span>
            <span class="btn-wrap" data-testid="finalize-dossier">
              <Button variant="primary" onclick={doFinalize} disabled={busy}>
                {busy ? "Finalizing…" : "Finalize"}
              </Button>
            </span>
          </div>
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
  td.req,
  td.claims {
    font-variant-numeric: tabular-nums;
  }
  td.conformance {
    color: var(--color-text-muted);
    white-space: nowrap;
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
  .objective {
    margin: 0.4rem 0 0;
    font-size: 0.9rem;
  }
  h4 {
    margin: 1rem 0 0.5rem;
    font-size: 0.9rem;
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }
  .count {
    font-size: 0.72rem;
    font-weight: 700;
    color: var(--color-accent);
    background: var(--color-accent-soft);
    border-radius: 999px;
    padding: 0.05rem 0.45rem;
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
  .claims-list {
    margin: 0;
    padding-left: 1.1rem;
    display: grid;
    gap: 0.25rem;
    font-size: 0.85rem;
  }
  code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    background: var(--color-surface);
    border-radius: 4px;
    padding: 0.05rem 0.35rem;
    overflow-wrap: anywhere;
  }
  .claims-list li {
    overflow-wrap: anywhere;
  }
  .error {
    color: var(--color-danger);
    font-size: 0.85rem;
    margin: 0.75rem 0 0;
  }
  .action-buttons {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    margin-top: 1rem;
    flex-wrap: wrap;
  }
  /* testid wrapper is layout-transparent so the Button is the flex item. */
  .btn-wrap {
    display: contents;
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
