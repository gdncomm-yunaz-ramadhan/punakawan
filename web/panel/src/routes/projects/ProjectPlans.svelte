<script lang="ts">
  import { onMount } from "svelte";
  import { listPlans, getPlan, type PlanSummary, type PlanDetail } from "../../lib/api/client";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  let plans: PlanSummary[] = $state([]);
  let loading = $state(true);
  let error: string | null = $state(null);

  // The selected plan's full manifest + version content, loaded lazily on
  // row click. Kept separate from the list so a detail load failure never
  // clears the list.
  let selectedId: string | null = $state(null);
  let detail: PlanDetail | null = $state(null);
  let detailLoading = $state(false);
  let detailError: string | null = $state(null);

  async function load() {
    loading = true;
    error = null;
    try {
      const res = await listPlans(projectId);
      plans = res.items;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function select(id: string) {
    if (selectedId === id) {
      // Toggle the detail closed on a second click.
      selectedId = null;
      detail = null;
      return;
    }
    selectedId = id;
    detail = null;
    detailError = null;
    detailLoading = true;
    try {
      detail = await getPlan(projectId, id);
    } catch (e) {
      detailError = e instanceof Error ? e.message : String(e);
    } finally {
      detailLoading = false;
    }
  }

  onMount(load);

  // Plan status is a free-form string, not an Availability value, so it
  // renders through StatusBadge's generic variant mode. Unknown statuses
  // fall back to a neutral pill rather than guessing a semantic color.
  function statusVariant(status: string): BadgeVariant {
    switch (status.toLowerCase()) {
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
            <th scope="col">Title</th>
            <th scope="col">Status</th>
            <th scope="col">Version</th>
            <th scope="col">Related tasks</th>
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
              <td class="title">{plan.title || plan.id}</td>
              <td><StatusBadge variant={statusVariant(plan.status)} label={plan.status} /></td>
              <td class="version">{plan.current_version || "—"}</td>
              <td class="tasks">{plan.related_tasks?.length ? plan.related_tasks.join(", ") : "—"}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    {#if selectedId}
      <div class="detail" data-testid="plan-detail">
        {#if detailLoading}
          <p>Loading plan…</p>
        {:else if detailError}
          <ErrorStateCard title="Failed to load plan" message={detailError} />
        {:else if detail}
          {@const m = detail.manifest}
          <header class="detail-head">
            <h3>{m.title || m.id}</h3>
            <StatusBadge variant={statusVariant(m.status)} label={m.status} />
          </header>
          {#if m.description}<p class="description">{m.description}</p>{/if}

          <dl class="meta">
            <dt>Plan ID</dt>
            <dd><code>{m.id}</code></dd>
            <dt>Current version</dt>
            <dd>{m.current_version || "—"}</dd>
            {#if m.related_tasks?.length}
              <dt>Related tasks</dt>
              <dd>{m.related_tasks.join(", ")}</dd>
            {/if}
            {#if m.derived_from?.knowledge?.length}
              <dt>Derived from knowledge</dt>
              <dd>{m.derived_from.knowledge.join(", ")}</dd>
            {/if}
            {#if m.derived_from?.workflows?.length}
              <dt>Derived from workflows</dt>
              <dd>{m.derived_from.workflows.join(", ")}</dd>
            {/if}
            {#if m.derived_from?.metadata?.length}
              <dt>Derived from metadata</dt>
              <dd>{m.derived_from.metadata.join(", ")}</dd>
            {/if}
          </dl>

          {#if detail.current_version_content}
            <h4>Current version content</h4>
            <pre class="content">{detail.current_version_content}</pre>
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
  td.version {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    white-space: nowrap;
  }
  td.tasks {
    color: var(--color-text-muted);
    word-break: break-word;
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
  .description {
    margin: 0.4rem 0 0;
    color: var(--color-text);
    font-size: 0.9rem;
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
