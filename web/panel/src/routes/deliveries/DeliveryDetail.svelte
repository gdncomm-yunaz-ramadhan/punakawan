<script lang="ts">
  import {
    getDeliveryView,
    cancelDelivery,
    type DeliveryView,
  } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import BentoGrid from "../../lib/components/cards/BentoGrid.svelte";
  import MetricCard from "../../lib/components/cards/MetricCard.svelte";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import Button from "../../lib/components/Button.svelte";
  import Dialog from "../../lib/components/overlay/Dialog.svelte";
  import DeliveryCancelDialog from "./DeliveryCancelDialog.svelte";

  interface Props {
    orchestrationId: string;
  }
  let { orchestrationId }: Props = $props();

  let view: DeliveryView | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(true);
  let cancelling = $state(false);
  let cancelError: string | null = $state(null);
  let confirmingCancel = $state(false);
  let projectListOpen = $state(false);
  let sessionListOpen = $state(false);
  let planListOpen = $state(false);

  async function load(id: string) {
    loading = true;
    error = null;
    try {
      view = await getDeliveryView(id);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    load(orchestrationId);
  });

  async function cancel() {
    if (!view) return;
    cancelling = true;
    cancelError = null;
    try {
      view = await cancelDelivery(orchestrationId, { expected_revision: view.orchestration.revision });
      confirmingCancel = false;
    } catch (e) {
      cancelError = e instanceof Error ? e.message : String(e);
    } finally {
      cancelling = false;
    }
  }

  function formatDate(value: string): string {
    return new Date(value).toLocaleString();
  }

  function formatDuration(seconds: number): string {
    const totalMinutes = Math.max(0, Math.floor(seconds / 60));
    const hours = Math.floor(totalMinutes / 60);
    const minutes = totalMinutes % 60;
    return hours > 0 ? `${hours}h ${minutes}m` : `${minutes}m`;
  }

  function sessionDuration(startedAt: string, endedAt?: string): string {
    const end = endedAt ? Date.parse(endedAt) : Date.now();
    const start = Date.parse(startedAt);
    return Number.isFinite(start) && Number.isFinite(end) ? formatDuration((end - start) / 1000) : "Not recorded";
  }

  function formatAmount(amount: number, currency: string): string {
    return new Intl.NumberFormat(undefined, { style: "currency", currency }).format(amount);
  }

  function totalsByCurrency(entries: { amount: number; currency: string }[]): Record<string, number> {
    return entries.reduce<Record<string, number>>((totals, { amount, currency }) => {
      totals[currency] = (totals[currency] ?? 0) + amount;
      return totals;
    }, {});
  }

  function estimatedCost(v: DeliveryView): string {
    const entries = (v.lifecycle?.usage ?? [])
      .filter((usage) => usage.kind === "estimate" && usage.cost_amount !== undefined && usage.cost_currency)
      .map((usage) => ({ amount: usage.cost_amount!, currency: usage.cost_currency! }));
    const totals = totalsByCurrency(entries);
    const formatted = Object.entries(totals).map(([currency, amount]) => formatAmount(amount, currency));
    return formatted.length > 0 ? formatted.join(" · ") : "No estimate";
  }

  const orchestrationStatusVariants: Record<string, BadgeVariant> = {
    pending: "neutral",
    active: "info",
    completed: "success",
    cancelled: "danger",
  };
</script>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <ErrorStateCard title="Failed to load delivery" message={error} />
{:else if view}
  {@const v = view}
  <PageHeader title={v.title} description={v.description || "No description recorded."}>
    {#snippet actions()}
      {#if v.orchestration.status === "pending" || v.orchestration.status === "active"}
        <Button variant="danger" size="sm" disabled={cancelling} onclick={() => (confirmingCancel = true)}>Cancel delivery</Button>
      {/if}
    {/snippet}
  </PageHeader>

  <DeliveryCancelDialog
    open={confirmingCancel}
    label={v.title}
    {orchestrationId}
    busy={cancelling}
    error={cancelError}
    onclose={() => !cancelling && (confirmingCancel = false)}
    onconfirm={cancel}
  />

  {#if cancelError && !confirmingCancel}
    <p role="alert" class="error">{cancelError}</p>
  {/if}

  <div class="status-row">
    <StatusBadge variant={orchestrationStatusVariants[v.orchestration.status] ?? "neutral"} label={v.orchestration.status} />
    <span class="meta">Created {formatDate(v.orchestration.created_at)}</span>
    <details class="technical-reference">
      <summary>Technical reference</summary>
      <code>{orchestrationId}</code>
    </details>
  </div>

  <section class="plan-overview" aria-labelledby="delivery-plan-heading">
    <h2 id="delivery-plan-heading">High-level plan</h2>
    {#if v.plan_id}
      <p><code>{v.plan_id} r{v.plan_revision ?? 1}</code></p>
    {:else if v.plan_record_id}
      <p><code>{v.plan_record_id}</code></p>
    {:else}
      <p class="empty">No high-level plan recorded.</p>
    {/if}
  </section>

  <BentoGrid>
    <button class="metric-button" type="button" onclick={() => (projectListOpen = true)} aria-label={`View ${v.projects.length} projects`}>
      <MetricCard size="small" columns={3} label="Projects" value={v.projects.length} />
    </button>
    <button class="metric-button" type="button" onclick={() => (sessionListOpen = true)} aria-label={`View ${v.lifecycle?.sessions.length ?? 0} sessions`}>
      <MetricCard size="small" columns={3} label="Sessions" value={v.lifecycle?.sessions.length ?? 0} />
    </button>
    <button class="metric-button" type="button" onclick={() => (planListOpen = true)} aria-label={`View ${v.project_plans?.length ?? 0} project plans`}>
      <MetricCard size="small" columns={3} label="Project plans" value={v.project_plans?.length ?? 0} />
    </button>
    <MetricCard size="small" columns={3} label="Estimated cost" value={estimatedCost(v)} />
  </BentoGrid>

  <section aria-labelledby="jira-heading">
    <h2 id="jira-heading">Jira activity</h2>
    {#if v.jira_activity?.length}
      <div class="table-wrap">
        <table>
          <caption>Jira activity</caption>
          <thead><tr><th scope="col">Issue</th><th scope="col">Activity</th><th scope="col">Reference</th><th scope="col">Recorded</th></tr></thead>
          <tbody>
            {#each v.jira_activity as activity (`${activity.event_type}-${activity.entity_id ?? ""}-${activity.fired_at}`)}
              <tr>
                <td>{activity.issue_key}</td>
                <td>{activity.event_type}</td>
                <td>{activity.entity_id || "—"}</td>
                <td>{formatDate(activity.fired_at)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else}
      <p class="empty">No Jira activity recorded.</p>
    {/if}
  </section>

  <Dialog open={projectListOpen} title="Projects in this delivery" onclose={() => (projectListOpen = false)}>
    {#if v.projects.length > 0}
      <div class="table-wrap">
        <table>
          <thead><tr><th scope="col">Project</th><th scope="col">Plans</th></tr></thead>
          <tbody>
            {#each v.projects as project (project.project_id)}
              <tr>
                <td>
                  <a href={`/projects/${encodeURIComponent(project.project_id)}`} onclick={(event) => { event.preventDefault(); navigate(`/projects/${encodeURIComponent(project.project_id)}`); }}>
                    {project.project_id}
                  </a>
                </td>
                <td>{(v.project_plans ?? []).filter((plan) => plan.project_id === project.project_id).length}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else}
      <p class="empty">No projects are linked to this delivery.</p>
    {/if}
  </Dialog>

  <Dialog open={sessionListOpen} title="Delivery sessions" onclose={() => (sessionListOpen = false)}>
    {#if v.lifecycle?.sessions.length}
      <div class="table-wrap">
        <table>
          <thead><tr><th scope="col">Agent</th><th scope="col">Path</th><th scope="col">Provider</th><th scope="col">Started</th><th scope="col">Duration</th></tr></thead>
          <tbody>
            {#each v.lifecycle.sessions as session (session.id)}
              <tr>
                <td>{session.participant}</td>
                <td><code>{session.worktree_path || "Not recorded"}</code></td>
                <td>{session.provider || "Not recorded"}</td>
                <td>{formatDate(session.started_at)}</td>
                <td>{sessionDuration(session.started_at, session.ended_at)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else}
      <p class="empty">No sessions recorded.</p>
    {/if}
  </Dialog>

  <Dialog open={planListOpen} title="Project plans in this delivery" onclose={() => (planListOpen = false)}>
    {#if (v.project_plans?.length ?? 0) > 0}
      <div class="table-wrap">
        <table>
          <thead><tr><th scope="col">Project</th><th scope="col">Plan ID</th><th scope="col">Created</th></tr></thead>
          <tbody>
            {#each v.project_plans ?? [] as plan (`${plan.project_id}-${plan.plan_id}-${plan.plan_revision}`)}
              <tr>
                <td>{plan.project_id}</td>
                <td>
                  <a
                    href={`/projects/${encodeURIComponent(plan.project_id)}?tab=plans&plan=${encodeURIComponent(plan.plan_id)}`}
                    onclick={(event) => {
                      event.preventDefault();
                      navigate(`/projects/${encodeURIComponent(plan.project_id)}?tab=plans&plan=${encodeURIComponent(plan.plan_id)}`);
                    }}
                  >{plan.plan_id} r{plan.plan_revision}</a>
                </td>
                <td>{formatDate(plan.created_at)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else}
      <p class="empty">No project plans linked to this delivery.</p>
    {/if}
  </Dialog>
{/if}

<style>
  .error { color: var(--color-danger); }
  .status-row { display: flex; align-items: center; flex-wrap: wrap; gap: 0.65rem; margin-bottom: 1.25rem; }
  .meta, .empty { color: var(--color-text-muted); font-size: 0.85rem; }
  .technical-reference { margin-left: auto; color: var(--color-text-muted); font-size: 0.8rem; }
  .technical-reference summary { cursor: pointer; }
  .technical-reference code { display: block; margin-top: 0.35rem; }
  .plan-overview { border-left: 3px solid var(--color-accent); padding-left: 0.85rem; margin: 0 0 1.25rem; }
  h2 { font-size: 1rem; margin: 1.5rem 0 0.7rem; }
  .plan-overview h2 { margin-top: 0; }
  .plan-overview p { margin: 0; }
  .metric-button { appearance: none; border: 0; background: transparent; padding: 0; text-align: left; cursor: pointer; border-radius: var(--radius-md); }
  .metric-button:focus-visible { outline: 2px solid var(--color-accent); outline-offset: 3px; }
  .metric-button:hover :global(.metric) { color: var(--color-accent); }
  .table-wrap { overflow-x: auto; border: 1px solid var(--color-border); border-radius: var(--radius-sm); }
  table { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
  th, td { padding: 0.65rem 0.75rem; text-align: left; border-bottom: 1px solid var(--color-border); vertical-align: top; }
  tr:last-child td { border-bottom: 0; }
  th { color: var(--color-text-muted); font-size: 0.72rem; letter-spacing: 0.04em; text-transform: uppercase; white-space: nowrap; }
  a { color: var(--color-accent); font-weight: 600; text-decoration: none; }
  a:hover { text-decoration: underline; }
  code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; word-break: break-word; }
</style>
