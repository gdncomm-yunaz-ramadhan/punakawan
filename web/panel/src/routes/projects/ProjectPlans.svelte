<script lang="ts">
  import { onMount } from "svelte";
  import { listPlans, getPlan, type PlanSummary, type PlanDetail } from "../../lib/api/client";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import Dialog from "../../lib/components/overlay/Dialog.svelte";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  let plans: PlanSummary[] = $state([]);
  let loading = $state(true);
  let error: string | null = $state(null);

  // The selected plan's exact revision + linked deliveries, loaded lazily
  // on row click. Kept separate from the list so a detail load failure
  // never clears the list.
  let selectedId: string | null = $state(null);
  let selectedRevision: number | null = $state(null);
  let detail: PlanDetail | null = $state(null);
  let detailLoading = $state(false);
  let detailError: string | null = $state(null);

  // Reads both ?plan= and ?revision= via URLSearchParams - never a
  // path-embedded pseudo-segment - so a link that names an exact delivery
  // revision (e.g. from DeliveryDetail's plans tab) opens that same exact
  // revision here instead of silently substituting the lineage's current
  // head.
  function requestedPlan(): { id: string | null; revision: number | null } {
    if (typeof window === "undefined") return { id: null, revision: null };
    const params = new URL(window.location.href).searchParams;
    const revisionParam = params.get("revision");
    const revision = revisionParam !== null ? Number(revisionParam) : null;
    return { id: params.get("plan"), revision: Number.isFinite(revision) ? revision : null };
  }

  async function load() {
    loading = true;
    error = null;
    try {
      const res = await listPlans(projectId);
      // Guard against a null/absent items array (a Go nil slice marshals to
      // JSON `null`, not `[]`): a project with no plans must render the
      // empty state, never trip the catch below into "Failed to load plans".
      plans = Array.isArray(res?.items) ? res.items : [];
      const requested = requestedPlan();
      if (requested.id && plans.some((plan) => plan.id === requested.id)) {
        await select(requested.id, requested.revision ?? undefined);
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  function closeDetail() {
    selectedId = null;
    selectedRevision = null;
    detail = null;
  }

  async function select(id: string, revision?: number) {
    if (selectedId === id && (revision === undefined || revision === selectedRevision)) {
      // Toggle the modal closed on a second click of the same row/revision.
      closeDetail();
      return;
    }
    selectedId = id;
    selectedRevision = revision ?? null;
    detail = null;
    detailError = null;
    detailLoading = true;
    try {
      const next = await getPlan(projectId, id, revision);
      if (!next?.plan?.id) throw new Error("Plan detail is unavailable.");
      detail = next;
    } catch (e) {
      detailError = e instanceof Error ? e.message : String(e);
    } finally {
      detailLoading = false;
    }
  }

  onMount(load);

  // The dialog's title bar needs a string before the detail payload has
  // loaded (or if it fails), so it falls back to the plan id rather than
  // sitting blank.
  let dialogTitle = $derived.by(() => {
    const p = detail?.plan;
    if (p) return p.objective || p.id;
    return selectedId ?? "Plan detail";
  });

  // Plan status is a free-form string, not an Availability value, so it
  // renders through StatusBadge's generic variant mode. Unknown statuses
  // fall back to a neutral pill rather than guessing a semantic color.
  function statusVariant(status?: string | null): BadgeVariant {
    switch (status?.toLowerCase()) {
      case "active":
      case "approved":
      case "accepted":
        return "success";
      case "draft":
      case "pending":
      case "in_review":
        return "warning";
      case "rejected":
      case "failed":
        return "danger";
      case "superseded":
      case "archived":
        return "info";
      default:
        return "neutral";
    }
  }
</script>

<section aria-label="Project plans">
  {#if loading}
    <p>Loading…</p>
  {:else if error}
    <ErrorStateCard title="Failed to load plans" message={error} />
  {:else if plans.length === 0}
    <EmptyStateCard
      title="No plans yet"
      message="Plans authored through the review protocol will appear here."
    />
  {:else}
    <div class="table-scroll">
      <table>
        <thead>
          <tr>
            <th scope="col">Objective</th>
            <th scope="col">Status</th>
            <th scope="col">Revision</th>
            <th scope="col">Linked deliveries</th>
          </tr>
        </thead>
        <tbody>
          {#each plans as plan (plan.id)}
            <tr
              class="row"
              class:selected={selectedId === plan.id}
              role="button"
              tabindex="0"
              aria-expanded={selectedId === plan.id}
              onclick={() => select(plan.id)}
              onkeydown={(e) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), select(plan.id))}
              data-testid={`plan-row-${plan.id}`}
            >
              <td class="title">{plan.objective || plan.id}</td>
              <td><StatusBadge variant={statusVariant(plan.status)} label={plan.status || "Not recorded"} /></td>
              <td class="version">r{plan.current_revision || "—"}</td>
              <td class="tasks">{plan.linked_deliveries?.length ?? 0}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>

<Dialog open={selectedId !== null} title={dialogTitle} onclose={closeDetail}>
  {#if detailLoading}
    <p>Loading plan…</p>
  {:else if detailError}
    <ErrorStateCard title="Failed to load plan" message={detailError} />
  {:else if detail}
    {@const p = detail.plan}
    <StatusBadge variant={statusVariant(p.status)} label={p.status || "Not recorded"} />

    <dl class="meta">
      <dt>Plan ID</dt>
      <dd><code>{p.id}</code></dd>
      <dt>Revision</dt>
      <dd>r{p.revision}</dd>
      {#if p.requirements?.length}
        <dt>Requirements</dt>
        <dd>{p.requirements.join(", ")}</dd>
      {/if}
      {#if p.acceptance_criteria?.length}
        <dt>Acceptance criteria</dt>
        <dd>{p.acceptance_criteria.join(", ")}</dd>
      {/if}
      {#if detail.linked_deliveries?.length}
        <dt>Linked deliveries</dt>
        <dd>
          {#each detail.linked_deliveries as link (`${link.orchestration_id}-${link.plan_revision}`)}
            <div>{link.orchestration_id} ({link.scope} r{link.plan_revision})</div>
          {/each}
        </dd>
      {/if}
    </dl>

    {#if !detail.linked_deliveries?.length}
      <EmptyStateCard title="No deliveries linked to this plan." message="This plan remains available for this project." />
    {/if}

    {#if p.legacy_markdown}
      <h4>Imported content</h4>
      <pre class="content">{p.legacy_markdown}</pre>
    {/if}
  {/if}
</Dialog>

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
  td.version {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    white-space: nowrap;
  }
  td.tasks {
    color: var(--color-text-muted);
    word-break: break-word;
  }
  dl.meta {
    margin: 0.9rem 0 0;
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.35rem 0.85rem;
    align-items: baseline;
    font-size: 0.85rem;
  }
  dl.meta dt {
    color: var(--color-text-muted);
    font-weight: 600;
  }
  dl.meta dd {
    margin: 0;
    word-break: break-word;
  }
  code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    background: var(--color-surface);
    border-radius: 4px;
    padding: 0.05rem 0.35rem;
  }
  h4 {
    margin: 1rem 0 0.35rem;
    font-size: 0.9rem;
  }
  pre.content {
    margin: 0;
    padding: 0.75rem 0.9rem;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.8rem;
    white-space: pre-wrap;
    word-break: break-word;
    overflow-x: auto;
  }
</style>
